package integration_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_ReviewPR3998_NexusUploadWithoutRead verifies that a user
// with upload rights but no blob-read rights can add a fresh local resource.
func Test_Integration_ReviewPR3998_NexusUploadWithoutRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Nexus integration test in short mode")
	}
	r := require.New(t)
	ctx := t.Context()

	container, err := testcontainers.Run(ctx, nexusImage,
		testcontainers.WithExposedPorts("8081/tcp", nexusDockerConnector),
		testcontainers.WithEnv(map[string]string{
			"INSTALL4J_ADD_VM_PARAMS": "-Xms1g -Xmx1g -XX:MaxDirectMemorySize=1g -Djava.util.prefs.userRoot=/nexus-data/javaprefs",
		}),
		testcontainers.WithWaitStrategyAndDeadline(5*time.Minute,
			wait.ForHTTP("/service/rest/v1/status/writable").WithPort("8081/tcp").WithStartupTimeout(5*time.Minute)),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })

	apiEndpoint, err := container.PortEndpoint(ctx, "8081/tcp", "")
	r.NoError(err)
	baseURL := "http://" + apiEndpoint
	rc, err := container.CopyFileFromContainer(ctx, "/nexus-data/admin.password")
	r.NoError(err)
	passwordRaw, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
	adminPassword := string(bytes.TrimSpace(passwordRaw))

	eulaURL := baseURL + "/service/rest/v1/system/eula"
	status, body := nexusAdminRequest(t, http.MethodGet, eulaURL, adminPassword, nil)
	if status != http.StatusNotFound {
		r.Equal(http.StatusOK, status, string(body))
		body = bytes.Replace(body, []byte(`"accepted" : false`), []byte(`"accepted" : true`), 1)
		body = bytes.Replace(body, []byte(`"accepted":false`), []byte(`"accepted":true`), 1)
		status, body = nexusAdminRequest(t, http.MethodPost, eulaURL, adminPassword, body)
		r.Equal(http.StatusNoContent, status, string(body))
	}

	api := func(method, path, payload string, want int) {
		t.Helper()
		status, body := nexusAdminRequest(t, method, baseURL+"/service/rest/v1"+path, adminPassword, []byte(payload))
		r.Equal(want, status, string(body))
	}
	api(http.MethodPut, "/security/realms/active", `["NexusAuthenticatingRealm","DockerToken"]`, http.StatusNoContent)
	api(http.MethodPost, "/repositories/docker/hosted", `{"name":"docker-hosted","online":true,"storage":{"blobStoreName":"default","strictContentTypeValidation":false,"writePolicy":"allow_once"},"docker":{"v1Enabled":false,"forceBasicAuth":true,"httpPort":5000}}`, http.StatusCreated)
	api(http.MethodPost, "/security/content-selectors", `{"name":"review-login-only","description":"Read only Docker login endpoint","expression":"path == \"/v2/\""}`, http.StatusNoContent)
	api(http.MethodPost, "/security/privileges/repository-content-selector", `{"name":"review-login-read","description":"Read only Docker login endpoint","format":"docker","repository":"docker-hosted","contentSelector":"review-login-only","actions":["read"]}`, http.StatusCreated)
	api(http.MethodPost, "/security/roles", `{"id":"review-upload-only","name":"Upload only","description":"Push without blob read","privileges":["review-login-read","nx-repository-view-docker-docker-hosted-add","nx-repository-view-docker-docker-hosted-edit"],"roles":[]}`, http.StatusOK)
	userPassword := generateRandomPassword(t, 32)
	api(http.MethodPost, "/security/users", `{"userId":"review-upload-only","firstName":"Review","lastName":"Upload","emailAddress":"review@example.invalid","password":"`+userPassword+`","status":"active","roles":["review-upload-only"]}`, http.StatusOK)

	dockerEndpoint, err := container.PortEndpoint(ctx, nexusDockerConnector, "")
	r.NoError(err)
	r.Eventually(func() bool {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+dockerEndpoint+"/v2/", nil)
		if reqErr != nil {
			return false
		}
		req.SetBasicAuth("review-upload-only", userPassword)
		resp, reqErr := http.DefaultClient.Do(req)
		if reqErr != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Minute, 2*time.Second)

	newResolver := func(user, password string) *urlresolver.CachingResolver {
		t.Helper()
		resolver, resolveErr := urlresolver.New(
			urlresolver.WithBaseURL(dockerEndpoint),
			urlresolver.WithPlainHTTP(true),
			urlresolver.WithBaseClient(createAuthClient(dockerEndpoint, user, password)),
		)
		r.NoError(resolveErr)
		return resolver
	}
	limitedResolver := newResolver("review-upload-only", userPassword)
	adminResolver := newResolver("admin", adminPassword)

	// ORAS Push itself succeeds with these rights: it starts a POST/PUT upload.
	probe := []byte("review-pr3998-direct-push")
	probeDesc := ociImageSpecV1.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    digest.FromBytes(probe),
		Size:      int64(len(probe)),
	}
	probeRef := dockerEndpoint + "/review-pr3998/probe:1.0.0"
	limitedStore, err := limitedResolver.StoreForReference(ctx, probeRef)
	r.NoError(err)
	r.NoError(limitedStore.Push(ctx, probeDesc, bytes.NewReader(probe)))
	adminStore, err := adminResolver.StoreForReference(ctx, probeRef)
	r.NoError(err)
	probeReader, err := adminStore.Fetch(ctx, probeDesc)
	r.NoError(err)
	gotProbe, err := io.ReadAll(probeReader)
	r.NoError(err)
	r.NoError(probeReader.Close())
	r.Equal(probe, gotProbe)

	limitedRepo, err := oci.NewRepository(oci.WithResolver(limitedResolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)
	const component, version = "ocm.software/review-pr3998", "1.0.0"
	data := []byte("review-pr3998-fresh-resource")
	b := inmemory.New(bytes.NewReader(data))
	b.SetMediaType("application/octet-stream")
	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "resource", Version: version}},
		Type:        "test-resource",
		Relation:    descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			MediaType:      "application/octet-stream",
			LocalReference: digest.FromBytes(data).String(),
		},
	}
	uploaded, err := limitedRepo.AddLocalResource(ctx, component, version, res, b)
	r.NoError(err, "a fresh blob should not require blob-read permission before upload")
	r.NotNil(uploaded)

	componentRef := adminResolver.ComponentVersionReference(ctx, component, version)
	adminStore, err = adminResolver.StoreForReference(ctx, componentRef)
	r.NoError(err)
	resourceDesc := ociImageSpecV1.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	resourceReader, err := adminStore.Fetch(ctx, resourceDesc)
	r.NoError(err)
	gotResource, err := io.ReadAll(resourceReader)
	r.NoError(err)
	r.NoError(resourceReader.Close())
	r.Equal(data, gotResource)
}
