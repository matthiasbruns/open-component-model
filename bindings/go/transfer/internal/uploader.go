package internal

import (
	"encoding/json"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3v1alpha1 "ocm.software/open-component-model/bindings/go/s3/spec/transformation/v1alpha1"
	coordinatepkg "ocm.software/open-component-model/bindings/go/transfer/coordinate"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// graphOptions carries optional inputs for building the transformation graph.
type graphOptions struct {
	// coordinates produces a neutral coordinate from a source access (dispatched per source type).
	coordinates *coordinatepkg.Registry
	// target places a coordinate into the S3 environment (used when UploadType is s3).
	target repository.Coordinator
}

// BuildOption configures optional graph-building behavior.
type BuildOption func(*graphOptions)

// WithS3Upload enables coordinate-based upload to S3: source accesses are reduced to a coordinate
// via the registry, and placed into S3 via the target Coordinator.
func WithS3Upload(coordinates *coordinatepkg.Registry, target repository.Coordinator) BuildOption {
	return func(o *graphOptions) {
		o.coordinates = coordinates
		o.target = target
	}
}

// WithCoordinates enables coordinate-based localization of external accesses (e.g. downloading an
// S3 source into a localBlob), stamping the coordinate and origin. It does not configure an upload
// target; use [WithS3Upload] for S3 as a target.
func WithCoordinates(coordinates *coordinatepkg.Registry) BuildOption {
	return func(o *graphOptions) {
		o.coordinates = coordinates
	}
}

func newGraphOptions(opts ...BuildOption) *graphOptions {
	o := &graphOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// s3UploadReady reports whether coordinate-based S3 upload is configured.
func (o *graphOptions) s3UploadReady() bool {
	return o != nil && o.coordinates != nil && o.target != nil
}

// processOCIToS3 emits the coordinate-based transfer of an OCI-accessed resource into S3:
// GetOCIArtifact (download) -> AddS3Resource (upload to the coordinate-derived S3 location). The
// original OCI access is kept as origin in a reference-hint label appended to the resource labels.
func processOCIToS3(resource descriptorv2.Resource, sourceAccess runtime.Typed, o *graphOptions, id string, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	// source access -> neutral coordinate, completed from the resource version.
	coord, err := o.coordinates.ToCoordinate(sourceAccess)
	if err != nil {
		return fmt.Errorf("producing coordinate from source access: %w", err)
	}
	if coord.Version == "" {
		coord.Version = resource.Version
	}

	// neutral coordinate -> S3 target access.
	targetAccess, err := o.target.FromCoordinate(coord)
	if err != nil {
		return fmt.Errorf("placing coordinate into s3 target: %w", err)
	}
	accessMap, err := toMap(targetAccess)
	if err != nil {
		return fmt.Errorf("encoding target access: %w", err)
	}

	// build the reference hint (coordinate + typed origin) and append it as a non-signed label.
	origin, err := toRaw(sourceAccess)
	if err != nil {
		return fmt.Errorf("capturing origin access: %w", err)
	}
	labels, err := labelsWithHint(resource.Labels, descruntime.ReferenceHint{Coordinate: *coord, Origin: origin})
	if err != nil {
		return fmt.Errorf("building labels with reference hint: %w", err)
	}

	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	// GetOCIArtifact (download the artifact to a file).
	getSpec, err := runtime.UnstructuredFromMixedData(map[string]any{"resource": resource})
	if err != nil {
		return fmt.Errorf("cannot create spec for GetOCIArtifact transformation: %w", err)
	}
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{Type: ociv1alpha1.GetOCIArtifactV1alpha1, ID: getResourceID},
		Spec:               getSpec,
	})

	// AddS3Resource (upload the file to the coordinate-derived S3 location).
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{Type: s3v1alpha1.AddS3ResourceV1alpha1, ID: addResourceID},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource": map[string]any{
				"name":          fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":       fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":          fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation":      fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access":        accessMap,
				"digest":        fmt.Sprintf("${has(%s.output.resource.digest) ? %s.output.resource.digest : null}", getResourceID, getResourceID),
				"labels":        labels,
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	})

	resourceTransformIDs[i] = addResourceID
	return nil
}

// processS3ToLocalBlob emits the by-value localization of an S3-accessed resource into a localBlob:
// GetS3Resource (download) -> AddLocalResource (store in the target CV repo). The coordinate is
// recorded both as the localBlob referenceName (so the existing localBlob->OCI restore can re-home
// it) and, together with the typed origin, as a reference-hint label.
func processS3ToLocalBlob(resource descriptorv2.Resource, sourceAccess runtime.Typed, o *graphOptions, component, version, id string, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	coord, err := o.coordinates.ToCoordinate(sourceAccess)
	if err != nil {
		return fmt.Errorf("producing coordinate from source access: %w", err)
	}
	// If the resource already carries a reference hint (e.g. from a prior oci -> s3 hop), reuse its
	// coordinate so the original identity survives the airgap instead of being derived from the key.
	if existing := coordinateFromHint(resource.Labels); existing != nil {
		coord = existing
	}
	if coord.Version == "" {
		coord.Version = resource.Version
	}
	origin, err := toRaw(sourceAccess)
	if err != nil {
		return fmt.Errorf("capturing origin access: %w", err)
	}
	labels, err := labelsWithHint(resource.Labels, descruntime.ReferenceHint{Coordinate: *coord, Origin: origin})
	if err != nil {
		return fmt.Errorf("building labels with reference hint: %w", err)
	}

	// referenceName carries the coordinate in OCI-shaped form so the existing localBlob->OCI restore works.
	referenceName := coord.Path
	if coord.Version != "" {
		referenceName += ":" + coord.Version
	}

	addLocalResourceType, err := chooseAddLocalResourceType(toSpec)
	if err != nil {
		return fmt.Errorf("choosing add local resource type for target: %w", err)
	}
	toRepo, err := asUnstructured(toSpec)
	if err != nil {
		return fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}

	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	getSpec, err := runtime.UnstructuredFromMixedData(map[string]any{"resource": resource})
	if err != nil {
		return fmt.Errorf("cannot create spec for GetS3Resource transformation: %w", err)
	}
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{Type: s3v1alpha1.GetS3ResourceV1alpha1, ID: getResourceID},
		Spec:               getSpec,
	})

	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{Type: addLocalResourceType, ID: addResourceID},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": toRepo.Data,
			"component":  component,
			"version":    version,
			"resource": map[string]any{
				"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access": map[string]any{
					"type":          descruntime.GetLocalBlobAccessType().String(),
					"referenceName": referenceName,
				},
				"digest":        fmt.Sprintf("${has(%s.output.resource.digest) ? %s.output.resource.digest : null}", getResourceID, getResourceID),
				"labels":        labels,
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	})

	resourceTransformIDs[i] = addResourceID
	return nil
}

// coordinateFromHint returns the coordinate from a resource's reference-hint label, if present.
func coordinateFromHint(labels []descriptorv2.Label) *descruntime.Coordinate {
	for _, l := range labels {
		if l.Name == coordinatepkg.ReferenceHintLabel {
			var hint descruntime.ReferenceHint
			if err := json.Unmarshal(l.Value, &hint); err == nil {
				c := hint.Coordinate
				return &c
			}
		}
	}
	return nil
}

func toMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func toRaw(access runtime.Typed) (*runtime.Raw, error) {
	data, err := json.Marshal(access)
	if err != nil {
		return nil, err
	}
	raw := &runtime.Raw{Data: data}
	raw.SetType(access.GetType())
	return raw, nil
}

// labelsWithHint returns a literal labels array = the source labels plus the reference-hint label.
// It is a build-time literal (option a): the source labels are known here, so no CEL append is needed.
func labelsWithHint(source []descriptorv2.Label, hint descruntime.ReferenceHint) ([]any, error) {
	hintValue, err := json.Marshal(hint)
	if err != nil {
		return nil, err
	}
	all := make([]descriptorv2.Label, 0, len(source)+1)
	all = append(all, source...)
	all = append(all, descriptorv2.Label{Name: coordinatepkg.ReferenceHintLabel, Value: hintValue})

	data, err := json.Marshal(all)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
