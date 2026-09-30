package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func reviewPR3702RawResource(name, typ string, access string) descriptor.Resource {
	t, _ := runtime.TypeFromString(typ)
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"}},
		Type:        "blob",
		Relation:    descriptor.LocalRelation,
		Access:      &runtime.Raw{Type: t, Data: []byte(access)},
	}
}

func reviewPR3702Build(t *testing.T, res descriptor.Resource, uploaders []transferv1alpha1.UploaderConfig) (*[]runtime.Type, error) {
	t.Helper()
	desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{res}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, uploaders)
	if err != nil {
		return nil, err
	}
	types := transformationTypes(tgd)
	return &types, nil
}

// C1: the local blob uploader's default match selects every S3 access (isType("S3")
// matches S3/v1 and S3/v2), but processResource only downloads S3/v2. A legacy s3/v1
// resource therefore fails the whole build under the catch-all that replaces
// copyMode: allResources / --copy-resources, where the base skipped it (kept by reference).
func TestReviewPR3702_C1_S3V1WithLocalBlobCatchAll(t *testing.T) {
	r := require.New(t)
	res := reviewPR3702RawResource("legacy-s3", "s3/v1", `{"type":"s3/v1","bucket":"b","key":"k","region":"eu-central-1"}`)

	types, err := reviewPR3702Build(t, res, withLocalBlobUploader())
	r.NoError(err, "the default local blob match must not select an access type the local blob uploader cannot copy")
	r.Equal([]runtime.Type{ociv1alpha1.OCIAddComponentVersionV1alpha1}, *types, "s3/v1 stays by reference, as with copyMode: allResources before")
}

// C2: an OCI-manifest-less local blob that carries a referenceName but no mediaType
// makes DefaultOCIUploaderMatch error ("no such key: mediaType") instead of evaluating
// to false, so the default OCI uploader (and the --upload-as ociArtifact translation)
// fails the whole build. The base treated an empty media type as "not an OCI manifest"
// and copied the blob as a local blob.
func TestReviewPR3702_C2_LocalBlobWithoutMediaType(t *testing.T) {
	r := require.New(t)
	res := reviewPR3702RawResource("no-media-type", "localBlob/v1", `{"type":"localBlob/v1","localReference":"sha256:abc123","referenceName":"org/blob:1.0.0"}`)

	types, err := reviewPR3702Build(t, res, withLocalBlobUploader(ociUploaders()...))
	r.NoError(err, "a local blob without mediaType is not an OCI manifest; the OCI uploader match must evaluate to false")
	r.Equal([]runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType}, *types)
}

// C3: has(resource.access.referenceName) is true for an explicitly empty referenceName,
// so the default OCI uploader selects the blob and the default imageReference becomes
// "<baseUrl>/" (no repository). The base required acc.ReferenceName != "" and copied the
// blob as a local blob.
func TestReviewPR3702_C3_EmptyReferenceName(t *testing.T) {
	r := require.New(t)
	res := reviewPR3702RawResource("empty-ref", "localBlob/v1", `{"type":"localBlob/v1","localReference":"sha256:abc123","mediaType":"application/vnd.oci.image.manifest.v1+json","referenceName":""}`)

	types, err := reviewPR3702Build(t, res, withLocalBlobUploader(ociUploaders()...))
	r.NoError(err)
	r.Equal([]runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType}, *types,
		"an empty referenceName must not select the OCI uploader (old: acc.ReferenceName != \"\")")
}
