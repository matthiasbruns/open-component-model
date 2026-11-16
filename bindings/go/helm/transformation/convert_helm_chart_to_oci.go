package transformation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/internal"
	"ocm.software/open-component-model/bindings/go/helm/internal/oci"
	"ocm.software/open-component-model/bindings/go/helm/spec/access"
	"ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociv2 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository/integrity"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ConvertHelmChartToOCI struct {
	Scheme *runtime.Scheme
}

func (t *ConvertHelmChartToOCI) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.ConvertHelmToOCI
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to convert helm transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for convert helm transformation")
	}
	if transformation.Spec.Resource == nil || transformation.Spec.Resource.Access == nil {
		return nil, fmt.Errorf("spec.resource and spec.resource.access are required for convert helm transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.ConvertHelmToOCIOutput{}
	}

	var helmAccess v1.Helm
	if err := access.Scheme.Convert(transformation.Spec.Resource.Access, &helmAccess); err != nil {
		return nil, fmt.Errorf("failed converting resource spec to v1.Helm: %w", err)
	}

	if transformation.Spec.ChartFile.URI == "" {
		return nil, fmt.Errorf("spec.chartFile.uri is required for convert helm transformation")
	}

	var (
		chartSpec *filesystem.Blob
		provSpec  *filesystem.Blob
		err       error
	)

	chartPath, err := filesystem.FilePathFromURI(transformation.Spec.ChartFile.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid chart file URI: %w", err)
	}
	if chartSpec, err = filesystem.GetBlobFromOSPath(chartPath); err != nil {
		return nil, fmt.Errorf("failed creating blob from chart file spec: %w", err)
	}
	slog.DebugContext(ctx, "Created blob for chart file", "uri", transformation.Spec.ChartFile.URI)

	if transformation.Spec.ProvFile != nil && transformation.Spec.ProvFile.URI != "" {
		provPath, err := filesystem.FilePathFromURI(transformation.Spec.ProvFile.URI)
		if err != nil {
			return nil, fmt.Errorf("invalid prov file URI: %w", err)
		}
		if provSpec, err = filesystem.GetBlobFromOSPath(provPath); err != nil {
			return nil, fmt.Errorf("failed creating blob from prov file spec: %w", err)
		}
		slog.DebugContext(ctx, "Created blob for prov file", "uri", transformation.Spec.ProvFile.URI)
	}

	outputPath, err := DetermineOutputPath(transformation.Spec.OutputPath, "oci")
	if err != nil {
		return nil, fmt.Errorf("error getting OCI output path: %w", err)
	}
	slog.DebugContext(ctx, "Going to use oci output path", "path", outputPath)

	result, err := oci.CopyChartToOCILayout(ctx, &internal.ChartData{
		Name:      helmAccess.GetChartName(),
		Version:   helmAccess.GetVersion(),
		ChartBlob: chartSpec,
		ProvBlob:  provSpec,
	}, transformation.Spec.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI layout from helm chart: %w", err)
	}

	if result.Blob == nil {
		return nil, fmt.Errorf("OCI layout blob is required but was not returned from OCI layout creation")
	}
	if result.Desc == nil {
		return nil, fmt.Errorf("OCI layout descriptor is required but was not returned from OCI layout creation")
	}

	spec, err := filesystem.BlobToSpec(result.Blob, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed converting OCI layout blob to file spec: %w", err)
	}

	slog.InfoContext(ctx, "Got OCI manifest descriptor")

	transformation.Output.File = *spec
	transformation.Output.Resource = transformation.Spec.Resource
	transformation.Output.Resource.Type = access.HelmRepositoryType

	if transformation.Output.Resource.Digest, err = artifactDigest(transformation.Output.Resource.Digest, result.Desc.Digest); err != nil {
		return nil, fmt.Errorf("cannot convert helm chart of resource %q to an OCI artifact: %w", transformation.Output.Resource.Name, err)
	}
	slog.DebugContext(ctx, "Set OCI image imageReference digest in output resource", "value", transformation.Output.Resource.Digest.Value)

	imageReference, err := imageReferenceFromHelmAccess(helmAccess)
	if err != nil {
		return nil, fmt.Errorf("failed constructing OCI image imageReference from Helm access: %w", err)
	}
	slog.InfoContext(ctx, "Constructed OCI image imageReference from Helm access", "imageReference", imageReference)

	ociAccess := ociv2.OCIImage{
		Type: runtime.Type{
			Version: ociv2.LegacyTypeVersion,
			Name:    ociv2.LegacyType,
		},
		ImageReference: imageReference,
	}
	updatedAccess := runtime.Raw{}
	if err = ociaccess.Scheme.Convert(&ociAccess, &updatedAccess); err != nil {
		return nil, fmt.Errorf("failed converting OCIImage access back to resource access format: %w", err)
	}
	transformation.Output.Resource.Access = &updatedAccess

	return &transformation, nil
}

// artifactDigest returns the resource digest for the converted chart with the given manifest
// digest. A recorded digest is returned unchanged or rejected, never rewritten: signatures
// and parent reference digests cover it.
func artifactDigest(recorded *descv2.Digest, manifest godigest.Digest) (*descv2.Digest, error) {
	if recorded == nil {
		var hashAlgorithm string
		switch manifest.Algorithm() {
		case godigest.SHA256:
			hashAlgorithm = "SHA-256"
		case godigest.SHA512:
			hashAlgorithm = "SHA-512"
		default:
			return nil, fmt.Errorf("unsupported OCI manifest digest algorithm: %s", manifest.Algorithm())
		}
		return &descv2.Digest{
			HashAlgorithm:          hashAlgorithm,
			NormalisationAlgorithm: "ociArtifactDigest/v1",
			Value:                  manifest.Encoded(),
		}, nil
	}

	if strings.EqualFold(recorded.HashAlgorithm, descv2.NoDigest) || strings.EqualFold(recorded.NormalisationAlgorithm, descv2.ExcludeFromSignature) {
		return recorded, nil
	}
	want, err := integrity.ParseDigest(&descruntime.Digest{
		HashAlgorithm:          recorded.HashAlgorithm,
		NormalisationAlgorithm: recorded.NormalisationAlgorithm,
		Value:                  recorded.Value,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid recorded digest: %w", err)
	}
	if want == "" {
		return nil, fmt.Errorf("incomplete recorded digest: hashAlgorithm=%q, value=%q", recorded.HashAlgorithm, recorded.Value)
	}
	if want != manifest {
		return nil, fmt.Errorf("the OCI artifact has digest %s, but the resource records %s (%s), so the copy would no longer verify against its signature", manifest, want, recorded.NormalisationAlgorithm)
	}
	return recorded, nil
}

func imageReferenceFromHelmAccess(helmAccess v1.Helm) (string, error) {
	reference, err := helmAccess.ChartReference()
	if err != nil {
		return "", fmt.Errorf("failed getting chart reference from Helm access: %w", err)
	}
	lref, err := looseref.ParseReference(reference)
	if err != nil {
		return "", fmt.Errorf("failed parsing reference from Helm access: %w", err)
	}

	if lref.Repository == "" {
		return "", fmt.Errorf("repository is required in Helm access to construct OCI image reference")
	}

	// Use only the repository path (without registry/domain) as the OCI image reference.
	ref := lref.Repository
	if lref.Tag != "" {
		ref = ref + ":" + lref.Tag
	}

	return ref, nil
}
