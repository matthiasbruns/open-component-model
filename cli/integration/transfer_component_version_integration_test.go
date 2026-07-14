package integration

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3access "ocm.software/open-component-model/bindings/go/s3/spec/access"
	s3accessv1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	s3credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
	s3repository "ocm.software/open-component-model/bindings/go/s3/repository"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_TransferComponentVersion(t *testing.T) {
	r := require.New(t)
	// We run this parallel as it spins up a separate container
	t.Parallel()

	// 1. Setup Local OCIRegistry
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

	// 2. Configure OCM to point to this registry
	// We create a temporary ocmconfig.yaml
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
`, registry.Host, registry.Port, registry.User, registry.Password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// 3. Create a Source CTF Archive with a component version
	componentName := "ocm.software/test-component"
	componentVersion := "v1.0.0"

	// Create connection to registry for verification later
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	// We can use the 'add component-version' command to create a CTF archive easily
	// Or we manually construct one using constructor.yaml and 'add component-version' command targetting a ctf path

	constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: test-resource
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "Hello, World from Transfer Test!"
`, componentName, componentVersion)

	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")

	// Create source CTF
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "creation of source CTF should succeed")

	// 4. Run Transfer Command: CTF -> OCI OCIRegistry
	transferCMD := cmd.New()

	// Construct source ref: ctf::<path>//<component>:<version>
	// Because the "add" command creates a CTF structure, we can reference it directly.
	// The "add" command with ctf repository puts it into the directory.
	// We need to verify if "add" creates a valid repository structure on the fly or if we need to init it.
	// The previous add_component_version_integration_test.go suggests it works directly.

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD.SetArgs([]string{
		"transfer",
		"component-version",
		sourceRef,
		targetRef,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Executes transfer
	r.NoError(transferCMD.ExecuteContext(ctx), "transfer should succeed")

	// 5. Verification
	// Check if component exists in target registry
	desc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should be able to retrieve transferred component")
	r.Equal(componentName, desc.Component.Name)
	r.Equal(componentVersion, desc.Component.Version)
	r.Len(desc.Component.Resources, 1)
	r.Equal("test-resource", desc.Component.Resources[0].Name)
}

// Test_Integration_Transfer_S3Access verifies the easy s3 -> s3 transfer path: a component whose
// resource references an S3 object (external access) is transferred by reference between two CTF
// repositories, keeps its S3 access unchanged, and remains downloadable from the target (the blob
// stays in the still-reachable bucket; nothing is localized or re-uploaded).
func Test_Integration_Transfer_S3Access(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	m := internal.CreateMinIO(t)
	const bucket, key = "transfer-bucket", "blobs/data.bin"
	content := []byte("hello from s3 transfer")
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

	const componentName = "ocm.software/s3-transfer-component"
	const componentVersion = "v1.0.0"

	constructorContent := fmt.Sprintf(`
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
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	// Create the source CTF with the S3-access resource.
	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "creating source CTF with S3 access should succeed")

	// Transfer source CTF -> target CTF. Without --copy-resources the S3 resource is carried
	// by reference (its access is not a local blob, so it is not localized).
	targetCTF := filepath.Join(t.TempDir(), "target-ctf")
	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion),
		fmt.Sprintf("ctf::%s", targetCTF),
		"--config", cfgPath,
	})
	transferCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(transferCtx), "s3 -> s3 by-reference transfer should succeed")

	// The target descriptor keeps the resource with its original S3 access.
	fs, err := filesystem.NewFS(targetCTF, os.O_RDONLY)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "transferred component should exist in the target CTF")
	r.Len(desc.Component.Resources, 1)
	res := desc.Component.Resources[0]
	r.Equal("s3-blob", res.Name)
	r.Equal("S3/v1", res.Access.GetType().String(), "S3 access must be preserved by reference through transfer")

	// Downloading from the TARGET resolves the carried S3 access and fetches from the bucket.
	output := filepath.Join(t.TempDir(), "downloaded")
	downloadCMD := cmd.New()
	downloadCMD.SetArgs([]string{
		"download", "resource",
		fmt.Sprintf("ctf::%s//%s:%s", targetCTF, componentName, componentVersion),
		"--identity", "name=s3-blob,version=v1.0.0",
		"--output", output,
		"--config", cfgPath,
	})
	r.NoError(downloadCMD.ExecuteContext(ctx), "download from transfer target should resolve the S3 access")

	outputBlob, err := filesystem.GetBlobFromOSPath(output)
	r.NoError(err)
	dataStream, err := outputBlob.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { r.NoError(dataStream.Close()) })
	data, err := io.ReadAll(dataStream)
	r.NoError(err)
	r.Equal(content, data, "downloaded content from the target should match the S3 object")
}

// s3AirgapOriginLabel carries the original S3 access across the airgap so the far side can restore
// it. The final carrier is a design decision (ADR-0003 reference-hints rework); a non-signed,
// descriptor-level label is used here because it survives the CTF localBlob rebuild.
const s3AirgapOriginLabel = "ocm.software/origin"

// Test_Integration_Transfer_S3_Airgapped exercises the full s3 -> localBlob -> s3 round-trip:
//
//	Leg 1 (by value): download the object from the source bucket and store it as a local blob in a
//	                  CTF (the airgap medium), recording the original S3 access as origin metadata.
//	Airgap boundary:  re-open the CTF fresh, as a disconnected environment would.
//	Leg 2 (restore):  using only the carried origin and the local bytes, upload to a NEW bucket and
//	                  reconstruct an S3 access — never touching the source location.
//
// Both legs run against the S3 ResourceRepository (Download + Upload) and a real CTF.
func Test_Integration_Transfer_S3_Airgapped(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	m := internal.CreateMinIO(t)
	const sourceBucket, objectKey = "source-bucket", "artifacts/data.bin"
	const targetBucket = "target-bucket"
	content := []byte("airgapped s3 payload")
	m.CreateBucket(t, sourceBucket)
	m.CreateBucket(t, targetBucket)
	m.PutObject(t, sourceBucket, objectKey, content)

	s3repo := s3repository.NewResourceRepository()
	creds := &s3credv1.S3Credentials{
		Type:            s3credv1.S3CredentialsVersionedType,
		AccessKeyID:     m.User,
		SecretAccessKey: m.Password,
	}

	const componentName, componentVersion, resourceName = "ocm.software/airgap-s3", "v1.0.0", "s3-blob"

	sourceAccess := &s3accessv1.S3{
		Type:         s3access.V1VersionedType,
		Region:       "us-east-1",
		BucketName:   sourceBucket,
		ObjectKey:    objectKey,
		Endpoint:     m.Endpoint,
		UsePathStyle: true,
		MediaType:    "application/octet-stream",
	}

	// ---- Leg 1: s3 -> localBlob, stored in a CTF, origin recorded as a non-signed label.
	ctfDir := filepath.Join(t.TempDir(), "airgap-ctf")
	writeFS, err := filesystem.NewFS(ctfDir, os.O_RDWR|os.O_CREATE)
	r.NoError(err)
	writeRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(writeFS))))
	r.NoError(err)

	sourceRes := &descriptor.Resource{}
	sourceRes.Name = resourceName
	sourceRes.Version = componentVersion
	sourceRes.Type = "blob"
	sourceRes.Relation = descriptor.ExternalRelation
	sourceRes.Access = sourceAccess

	downloaded, err := s3repo.DownloadResource(ctx, sourceRes, creds)
	r.NoError(err)

	originJSON, err := json.Marshal(sourceAccess)
	r.NoError(err)

	localRes := &descriptor.Resource{}
	localRes.Name = resourceName
	localRes.Version = componentVersion
	localRes.Type = "blob"
	localRes.Relation = descriptor.LocalRelation
	localRes.Access = &v2.LocalBlob{MediaType: "application/octet-stream"}
	localRes.Labels = []descriptor.Label{{Name: s3AirgapOriginLabel, Value: originJSON, Signing: false}}

	stored, err := writeRepo.AddLocalResource(ctx, componentName, componentVersion, localRes, downloaded)
	r.NoError(err)

	desc := descriptor.Descriptor{}
	desc.Component.Name = componentName
	desc.Component.Version = componentVersion
	desc.Component.Provider.Name = "ocm.software"
	desc.Component.Resources = append(desc.Component.Resources, *stored)
	r.NoError(writeRepo.AddComponentVersion(ctx, &desc), "storing the localized resource in the CTF should succeed")

	// ---- Airgap boundary: re-open the CTF fresh (the disconnected environment).
	readFS, err := filesystem.NewFS(ctfDir, os.O_RDONLY)
	r.NoError(err)
	readRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(readFS))))
	r.NoError(err)

	carried, err := readRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(carried.Component.Resources, 1)
	carriedRes := carried.Component.Resources[0]

	var origin s3accessv1.S3
	foundOrigin := false
	for _, l := range carriedRes.Labels {
		if l.Name == s3AirgapOriginLabel {
			r.NoError(json.Unmarshal(l.Value, &origin))
			foundOrigin = true
		}
	}
	r.True(foundOrigin, "origin label must survive the airgap")
	r.Equal(sourceBucket, origin.BucketName, "origin must record the source bucket")

	// ---- Leg 2: localBlob -> s3, using ONLY the local bytes and the carried origin.
	localBytes, _, err := readRepo.GetLocalResource(ctx, componentName, componentVersion, carriedRes.ToIdentity())
	r.NoError(err)

	targetRes := &descriptor.Resource{}
	targetRes.Name = resourceName
	targetRes.Version = componentVersion
	targetRes.Type = "blob"
	targetRes.Relation = descriptor.ExternalRelation
	targetRes.Access = &s3accessv1.S3{
		Type:         s3access.V1VersionedType,
		Region:       origin.Region,       // shape recovered from origin
		BucketName:   targetBucket,         // new, disconnected-environment location
		ObjectKey:    origin.ObjectKey,
		Endpoint:     origin.Endpoint,
		UsePathStyle: origin.UsePathStyle,
		MediaType:    origin.MediaType,
	}

	uploaded, err := s3repo.UploadResource(ctx, targetRes, localBytes, creds)
	r.NoError(err, "re-upload to the target bucket should succeed")
	r.Equal("S3/v1", uploaded.Access.GetType().String(), "restored access must be an S3 access")

	// ---- Verify: the object is now in the target bucket with the original content.
	verifyRes := &descriptor.Resource{}
	verifyRes.Access = uploaded.Access
	back, err := s3repo.DownloadResource(ctx, verifyRes, creds)
	r.NoError(err)
	rc, err := back.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	got, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, got, "content must survive the full s3 -> localBlob -> s3 round-trip")
}

// Test_Integration_Transfer_OCI_To_S3 exercises the cross-tech oci -> s3 path: an OCI artifact is
// downloaded from a registry (as an OCI image layout) and uploaded to an S3 bucket, so the resource
// moves from an ociArtifact access to an S3 access. Both legs go through the ResourceRepository
// interface (OCI download, S3 upload), which is what makes the move tech-agnostic.
func Test_Integration_Transfer_OCI_To_S3(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	// Source: an OCI registry. Target: a MinIO bucket.
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
	const bucket, objectKey = "oci-artifacts", "images/myimage.tar"
	m.CreateBucket(t, bucket)
	s3repo := s3repository.NewResourceRepository()
	s3creds := &s3credv1.S3Credentials{
		Type:            s3credv1.S3CredentialsVersionedType,
		AccessKeyID:     m.User,
		SecretAccessKey: m.Password,
	}

	// Push an OCI image artifact into the source registry.
	ociRes := &descriptor.Resource{}
	ociRes.Name = "image"
	ociRes.Version = "v1.0.0"
	ociRes.Type = "ociImage"
	ociRes.Access = &ociaccessv1.OCIImage{
		Type:           runtime.NewVersionedType(ociaccessv1.LegacyType, ociaccessv1.LegacyTypeVersion),
		ImageReference: fmt.Sprintf("%s/myimage:v1.0.0", registry.RegistryAddress),
	}
	layoutTar := internal.CreateSingleLayerOCIImageLayoutTar(t, []byte("oci artifact payload"), "myimage:v1.0.0")
	pushed, err := ociRepo.UploadResource(ctx, ociRes, direct.NewFromBuffer(layoutTar, true))
	r.NoError(err, "pushing the OCI artifact should succeed")
	r.Equal("ociArtifact/v1", pushed.Access.GetType().String())

	// oci -> s3: download the OCI artifact, then upload the exact bytes to S3.
	ociBlob, err := ociRepo.DownloadResource(ctx, pushed)
	r.NoError(err, "downloading the OCI artifact should succeed")
	ociRC, err := ociBlob.ReadCloser()
	r.NoError(err)
	ociData, err := io.ReadAll(ociRC)
	r.NoError(err)
	r.NoError(ociRC.Close())
	r.NotEmpty(ociData, "downloaded OCI artifact should not be empty")

	s3Res := &descriptor.Resource{}
	s3Res.Name = "image"
	s3Res.Version = "v1.0.0"
	s3Res.Type = "ociImage"
	s3Res.Access = &s3accessv1.S3{
		Type:         s3access.V1VersionedType,
		Region:       "us-east-1",
		BucketName:   bucket,
		ObjectKey:    objectKey,
		Endpoint:     m.Endpoint,
		UsePathStyle: true,
		MediaType:    layout.MediaTypeOCIImageLayoutTarV1,
	}
	uploaded, err := s3repo.UploadResource(ctx, s3Res, direct.NewFromBytes(ociData), s3creds)
	r.NoError(err, "uploading the OCI artifact bytes to S3 should succeed")
	r.Equal("S3/v1", uploaded.Access.GetType().String(), "resource access is now an S3 access")

	// Verify: the OCI artifact now lives in S3, byte-identical to what OCI produced.
	verifyRes := &descriptor.Resource{}
	verifyRes.Access = uploaded.Access
	fromS3, err := s3repo.DownloadResource(ctx, verifyRes, s3creds)
	r.NoError(err)
	s3RC, err := fromS3.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(s3RC.Close()) }()
	s3Data, err := io.ReadAll(s3RC)
	r.NoError(err)
	r.Equal(ociData, s3Data, "the OCI artifact stored in S3 must match the artifact downloaded from OCI")
}

// Test_Integration_TransferComponentVersion_PreservesSignatures_ToOCI verifies that signatures
// on a component descriptor are preserved when transferring a component version with local blob
// resources to an OCI registry. This exercises the OCIAddComponentVersion transformer path.
func Test_Integration_TransferComponentVersion_PreservesSignatures_ToOCI(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	// 1. Setup target OCI registry
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

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
`, registry.Host, registry.Port, registry.User, registry.Password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// 2. Create a signed source CTF with a local blob resource using the Go API
	componentName := "ocm.software/test-signed-to-oci"
	componentVersion := "1.0.0"

	fromDesc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider:  descriptor.Provider{Name: "ocm.software"},
			Resources: []descriptor.Resource{},
		},
	}

	// Add a local blob resource
	fromDesc.Component.Resources = []descriptor.Resource{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-blob",
					Version: "1.0.0",
				},
			},
			Type:     "plainText",
			Relation: descriptor.LocalRelation,
			Access: &v2.LocalBlob{
				MediaType: "text/plain",
			},
		},
	}

	// Generate a digest and add a signature
	dig, err := signing.GenerateDigest(t.Context(), fromDesc, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	fromDesc.Signatures = []descriptor.Signature{
		{
			Name:   "test-signature",
			Digest: *dig,
			Signature: descriptor.SignatureInfo{
				Algorithm: "RSASSA-PSS",
				Value:     "dGVzdC1zaWduYXR1cmUtdmFsdWU=",
				MediaType: "application/vnd.ocm.signature.rsa",
			},
		},
	}

	// Setup source CTF
	sourceCTFPath := filepath.Join(t.TempDir(), "source-ctf")
	fs, err := filesystem.NewFS(sourceCTFPath, os.O_RDWR|os.O_CREATE)
	r.NoError(err)
	archive := ctf.NewFileSystemCTF(fs)
	sourceRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)

	ctx := t.Context()

	// Add local blob resource data
	blobData := []byte("Hello, signed world for OCI!")
	updatedRes, err := sourceRepo.AddLocalResource(
		ctx, componentName, componentVersion,
		&fromDesc.Component.Resources[0],
		inmemory.New(bytes.NewReader(blobData)),
	)
	r.NoError(err)
	fromDesc.Component.Resources[0] = *updatedRes

	// Add the signed component version
	r.NoError(sourceRepo.AddComponentVersion(ctx, fromDesc))

	// 3. Transfer to OCI registry with --copy-resources
	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTFPath, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--copy-resources",
	})

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(ctx), "transfer to OCI registry should succeed")

	// 4. Verify signatures in target OCI registry
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	targetDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should be able to retrieve transferred component from OCI registry")
	r.Equal(componentName, targetDesc.Component.Name)
	r.Equal(componentVersion, targetDesc.Component.Version)

	// Verify signatures were preserved
	r.Len(targetDesc.Signatures, 1, "transferred descriptor should have 1 signature")
	r.Equal("test-signature", targetDesc.Signatures[0].Name)
	r.Equal(fromDesc.Signatures[0].Digest.HashAlgorithm, targetDesc.Signatures[0].Digest.HashAlgorithm)
	r.Equal(fromDesc.Signatures[0].Digest.Value, targetDesc.Signatures[0].Digest.Value)
	r.Equal("RSASSA-PSS", targetDesc.Signatures[0].Signature.Algorithm)
	r.Equal("dGVzdC1zaWduYXR1cmUtdmFsdWU=", targetDesc.Signatures[0].Signature.Value)

	// Verify resource was also transferred
	r.Len(targetDesc.Component.Resources, 1)
	r.Equal("test-blob", targetDesc.Component.Resources[0].Name)
}
