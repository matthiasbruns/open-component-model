package integration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3repository "ocm.software/open-component-model/bindings/go/s3/repository"
	s3credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
	transfercoord "ocm.software/open-component-model/bindings/go/transfer/coordinate"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Test_Integration_Transfer_UploadAs_S3 exercises the full CLI path: `transfer component-version
// --copy-resources --upload-as s3`, driven by an uploader.s3 config, moves an OCI-artifact resource
// into S3 via the coordinate system, keeping the original OCI access as origin. The component
// descriptor still lands in the CTF target.
func Test_Integration_Transfer_UploadAs_S3(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	// Source OCI registry + target MinIO bucket.
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	ociRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	m := internal.CreateMinIO(t)
	const bucket = "mirror-bucket"
	m.CreateBucket(t, bucket)

	const componentName, componentVersion = "ocm.software/upload-as-s3", "v1.0.0"

	// Push an OCI-artifact resource and add a source component version referencing it.
	ociRes := &descriptor.Resource{}
	ociRes.Name = "image"
	ociRes.Version = componentVersion
	ociRes.Type = "ociImage"
	ociRes.Relation = descriptor.ExternalRelation
	ociRes.Access = &ociaccessv1.OCIImage{
		Type:           runtime.NewVersionedType(ociaccessv1.LegacyType, ociaccessv1.LegacyTypeVersion),
		ImageReference: fmt.Sprintf("%s/acme/podinfo:6.7.1", registry.RegistryAddress),
	}
	layoutTar := internal.CreateSingleLayerOCIImageLayoutTar(t, []byte("oci artifact payload"), "podinfo:6.7.1")
	pushed, err := ociRepo.UploadResource(ctx, ociRes, direct.NewFromBuffer(layoutTar, true))
	r.NoError(err)
	// Reference the pushed artifact with an explicit http scheme so the transfer's OCI download
	// uses plain HTTP against the test registry (the resolver enables plain HTTP for http refs).
	pushed.Access = &ociaccessv1.OCIImage{
		Type:           runtime.NewVersionedType(ociaccessv1.LegacyType, ociaccessv1.LegacyTypeVersion),
		ImageReference: fmt.Sprintf("http://%s/acme/podinfo:6.7.1", registry.RegistryAddress),
	}

	sourceDesc := descriptor.Descriptor{}
	sourceDesc.Meta.Version = "v2"
	sourceDesc.Component.Name = componentName
	sourceDesc.Component.Version = componentVersion
	sourceDesc.Component.Provider.Name = "ocm.software"
	sourceDesc.Component.Resources = []descriptor.Resource{*pushed}
	r.NoError(ociRepo.AddComponentVersion(ctx, &sourceDesc))

	// Config: OCI registry creds (source), S3 creds (target), and the uploader.s3 target.
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: S3
      hostname: %[5]q
      port: %[6]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        accessKeyId: %[7]q
        secretAccessKey: %[8]q
- type: uploader.s3.ocm.software/v1alpha1
  bucket: %[9]s
  keyPrefix: oci
  region: us-east-1
  endpoint: %[10]s
  usePathStyle: true
`, registry.Host, registry.Port, registry.User, registry.Password,
		m.Host, m.Port, m.User, m.Password, bucket, m.Endpoint)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// transfer component-version --copy-resources --upload-as s3
	targetCTF := filepath.Join(t.TempDir(), "target-ctf")
	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, componentName, componentVersion),
		fmt.Sprintf("ctf::%s", targetCTF),
		"--copy-resources",
		"--upload-as", "s3",
		"--config", cfgPath,
	})
	r.NoError(transferCMD.ExecuteContext(ctx), "transfer --upload-as s3 should succeed")

	// The target descriptor resource now has an S3 access and a reference hint carrying the origin.
	fs, err := filesystem.NewFS(targetCTF, os.O_RDONLY)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(desc.Component.Resources, 1)
	res := desc.Component.Resources[0]
	r.Equal("S3/v1", res.Access.GetType().String(), "resource access must be rewritten to S3")

	hint, ok := transfercoord.ReferenceHintOf(&res)
	r.True(ok, "reference hint must be present")
	r.Equal("acme/podinfo", hint.Coordinate.Path)
	r.NotNil(hint.Origin, "origin must be preserved")
	r.Equal("ociArtifact/v1", hint.Origin.GetType().String())

	// The content really lives in S3.
	s3repo := s3repository.NewResourceRepository()
	s3creds := &s3credv1.S3Credentials{Type: s3credv1.S3CredentialsVersionedType, AccessKeyID: m.User, SecretAccessKey: m.Password}
	verify := &descriptor.Resource{}
	verify.Access = res.Access
	back, err := s3repo.DownloadResource(ctx, verify, s3creds)
	r.NoError(err)
	rc, err := back.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.NotEmpty(data, "the OCI artifact must be present in S3 after --upload-as s3")
}

// Test_Integration_Transfer_S3_To_LocalBlob exercises leg 1 of the airgapped cross-tech round-trip:
// `transfer component-version --copy-resources` localizes an S3-accessed resource into a localBlob in
// the target CTF, recording the coordinate (as referenceName, so the localBlob->OCI restore can
// re-home it) and the original S3 access as origin in a reference-hint label.
func Test_Integration_Transfer_S3_To_LocalBlob(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	m := internal.CreateMinIO(t)
	const bucket, key = "src-bucket", "artifacts/data.bin"
	content := []byte("airgap s3 payload")
	m.CreateBucket(t, bucket)
	m.PutObject(t, bucket, key, content)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: S3
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        accessKeyId: %[3]q
        secretAccessKey: %[4]q
`, m.Host, m.Port, m.User, m.Password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	const componentName, componentVersion = "ocm.software/airgap-s3-localize", "v1.0.0"

	constructor := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: s3-blob
    version: v1.0.0
    type: blob
    access:
      type: S3/v1
      bucketName: %s
      objectKey: %s
      endpoint: %s
      usePathStyle: true
      region: us-east-1
`, componentName, componentVersion, bucket, key, m.Endpoint)
	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), os.ModePerm))

	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "creating source CTF with an S3 access should succeed")

	// --copy-resources localizes the S3 resource into a localBlob in the target CTF.
	targetCTF := filepath.Join(t.TempDir(), "target-ctf")
	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion),
		fmt.Sprintf("ctf::%s", targetCTF),
		"--copy-resources",
		"--config", cfgPath,
	})
	r.NoError(transferCMD.ExecuteContext(ctx), "s3 -> localBlob localization should succeed")

	fs, err := filesystem.NewFS(targetCTF, os.O_RDONLY)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(desc.Component.Resources, 1)
	res := desc.Component.Resources[0]
	r.Contains(res.Access.GetType().String(), "ocalBlob", "the S3 resource must be localized to a localBlob")

	hint, ok := transfercoord.ReferenceHintOf(&res)
	r.True(ok, "reference hint must be present on the localized resource")
	r.Equal("artifacts/data.bin", hint.Coordinate.Path)
	r.NotNil(hint.Origin, "origin must be preserved")
	r.Equal("S3/v1", hint.Origin.GetType().String(), "origin is the original S3 access")

	// The content is stored as a local blob, matching the source object.
	blobData, _, err := repo.GetLocalResource(ctx, componentName, componentVersion, res.ToIdentity())
	r.NoError(err)
	rc2, err := blobData.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(rc2.Close()) }()
	got, err := io.ReadAll(rc2)
	r.NoError(err)
	r.Equal(content, got, "localized content must match the source S3 object")
}

// Test_Integration_Transfer_S3_To_LocalBlob_To_OCI exercises the full airgapped cross-tech
// round-trip in two hops:
//
//	hop 1 (leg in): transfer s3-source -> intermediate CTF --copy-resources
//	    localizes the S3-accessed OCI image layout into a localBlob, carrying the coordinate as
//	    the localBlob referenceName and the original S3 access as origin.
//	hop 2 (leg out): transfer intermediate CTF -> OCI registry --copy-resources --upload-as ociArtifact
//	    restores the layout-tar localBlob back into a real OCI artifact in the registry, keyed by
//	    the coordinate-derived referenceName.
//
// The S3 object is a full OCI image layout tar (media type
// application/vnd.ocm.software.oci.layout.v1+tar) so the localBlob is OCI-restorable in hop 2.
func Test_Integration_Transfer_S3_To_LocalBlob_To_OCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	// S3 source holding an OCI image layout tar, and an OCI registry as the final target.
	m := internal.CreateMinIO(t)
	const bucket, key = "src-bucket", "acme/podinfo"
	m.CreateBucket(t, bucket)
	layoutTar := internal.CreateSingleLayerOCIImageLayoutTar(t, []byte("oci artifact payload"), "podinfo:6.7.1")
	m.PutObject(t, bucket, key, layoutTar.Bytes())

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: S3
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        accessKeyId: %[3]q
        secretAccessKey: %[4]q
  - identity:
      type: OCIRegistry
      hostname: %[5]q
      port: %[6]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[7]q
        password: %[8]q
`, m.Host, m.Port, m.User, m.Password, registry.Host, registry.Port, registry.User, registry.Password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	const componentName, componentVersion = "ocm.software/airgap-s3-to-oci", "v1.0.0"

	// The S3 access carries the OCI image layout media type so the localBlob is restorable.
	constructor := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: image
    version: 6.7.1
    type: ociImage
    access:
      type: S3/v1
      bucketName: %s
      objectKey: %s
      mediaType: application/vnd.ocm.software.oci.layout.v1+tar
      endpoint: %s
      usePathStyle: true
      region: us-east-1
`, componentName, componentVersion, bucket, key, m.Endpoint)
	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), os.ModePerm))

	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "creating source CTF with an S3 access should succeed")

	// hop 1: s3 -> localBlob localization into the intermediate CTF.
	intermediateCTF := filepath.Join(t.TempDir(), "intermediate-ctf")
	hop1 := cmd.New()
	hop1.SetArgs([]string{
		"transfer", "component-version",
		fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion),
		fmt.Sprintf("ctf::%s", intermediateCTF),
		"--copy-resources",
		"--config", cfgPath,
	})
	r.NoError(hop1.ExecuteContext(ctx), "hop 1 (s3 -> localBlob) should succeed")

	// The intermediate resource is now a layout-tar localBlob carrying the coordinate + origin.
	fs, err := filesystem.NewFS(intermediateCTF, os.O_RDONLY)
	r.NoError(err)
	interRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	interDesc, err := interRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(interDesc.Component.Resources, 1)
	interRes := interDesc.Component.Resources[0]
	r.Contains(interRes.Access.GetType().String(), "ocalBlob", "hop 1 must localize to a localBlob")
	hint, ok := transfercoord.ReferenceHintOf(&interRes)
	r.True(ok, "reference hint must survive hop 1")
	r.Equal("acme/podinfo", hint.Coordinate.Path)
	r.Equal("S3/v1", hint.Origin.GetType().String())

	// hop 2: localBlob -> OCI artifact restore into the registry. The target uses an explicit
	// http scheme so the transfer pushes over plain HTTP against the test registry.
	targetRef := fmt.Sprintf("http://%s/airgap", registry.RegistryAddress)
	hop2 := cmd.New()
	hop2.SetArgs([]string{
		"transfer", "component-version",
		fmt.Sprintf("ctf::%s//%s:%s", intermediateCTF, componentName, componentVersion),
		targetRef,
		"--copy-resources",
		"--upload-as", "ociArtifact",
		"--config", cfgPath,
	})
	r.NoError(hop2.ExecuteContext(ctx), "hop 2 (localBlob -> oci artifact) should succeed")

	// The final registry resource is a real OCI artifact restored from the layout tar.
	targetClient := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	targetResolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithSubPath("airgap"),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(targetClient),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(targetResolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)
	finalDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(finalDesc.Component.Resources, 1)
	finalRes := finalDesc.Component.Resources[0]
	r.Equal("ociArtifact/v1", finalRes.Access.GetType().String(), "hop 2 must restore an OCI artifact access")
}
