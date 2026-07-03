package integration

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Test_Integration_AddComponentVersion_WgetInput exercises the wget input method through
// the CLI: `ocm add component-version` downloads a resource from an HTTP server and stores
// it as a local blob in the target OCI registry.
func Test_Integration_AddComponentVersion_WgetInput(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	content := []byte("hello from wget input")
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(content)
	}))
	t.Cleanup(fileSrv.Close)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: "` + registry.Host + `"
      port: "` + registry.Port + `"
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: "` + registry.User + `"
        password: "` + registry.Password + `"
`
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	const componentName = "ocm.software/wget-input-component"
	const componentVersion = "v1.0.0"

	constructorContent := `
components:
- name: ` + componentName + `
  version: ` + componentVersion + `
  provider:
    name: ocm.software
  resources:
  - name: remote-blob
    version: v1.0.0
    type: blob
    input:
      type: wget
      url: ` + fileSrv.URL + `/artifact.bin
      mediaType: application/octet-stream
`
	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", "http://" + registry.RegistryAddress,
		"--constructor", constructorPath,
		"--config", cfgPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	r.NoError(addCMD.ExecuteContext(ctx), "add component-version should succeed with wget input")

	// Read the component version back and verify the downloaded bytes were stored locally.
	repo := registry.Connect(t)
	desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(desc.Component.Resources, 1)
	res := desc.Component.Resources[0]
	r.Equal("remote-blob", res.Name)

	blobData, _, err := repo.GetLocalResource(ctx, componentName, componentVersion, res.ToIdentity())
	r.NoError(err)
	rc, err := blobData.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	got, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, got)
}
