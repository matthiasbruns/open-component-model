package resource

import (
	"testing"

	"github.com/oras-project/oras-go/v3/registry/remote/properties"
	"github.com/stretchr/testify/require"

	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// TestReviewPR4221_ResourcePathDockerHub shows the resource/upload path
// resolving Docker Hub credentials for every BaseUrl spelling, because
// credentialFunc delegates to oras' StaticCredentialFunc, which keys on
// resource.Host() and maps docker.io back to registry-1.docker.io.
//
// oci/credentials.CredentialFunc on the provider path compares against
// resource.Registry instead and resolves only the "docker.io" spelling
// (TestReviewPR4221_DockerHubIdentity). The two production credential paths
// therefore disagree about Docker Hub.
func TestReviewPR4221_ResourcePathDockerHub(t *testing.T) {
	for _, host := range []string{"docker.io", "registry-1.docker.io", "DOCKER.IO"} {
		t.Run(host, func(t *testing.T) {
			r := require.New(t)
			fn := credentialFunc(host, &ocicredsv1.OCICredentials{Username: "u", Password: "p"})
			// What v3 passes for a request dialled at registry-1.docker.io.
			cred, err := fn(t.Context(), properties.Resource{Registry: "docker.io", Path: "library/busybox"})
			r.NoError(err)
			r.Equal("u", cred.Username, "credential should resolve for BaseUrl %q", host)
		})
	}
}
