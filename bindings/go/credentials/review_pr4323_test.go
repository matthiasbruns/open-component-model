package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// A hostname-only entry next to the same host with an explicit standard port
// loads on main. Port inference on both sides makes the two entries match each
// other, which the graph rejects as a cycle, so the whole config fails to load.
func TestReviewPR4323_F1_HostnameOnlyAndStandardPortEntriesLoad(t *testing.T) {
	for _, port := range []string{"443", "80"} {
		t.Run("port "+port, func(t *testing.T) {
			r := require.New(t)
			graph, err := GetGraph(t, `
type: credentials.config.ocm.software
consumers:
  - identity:
      type: OCIRegistry
      hostname: "ghcr.io"
    credentials:
      - type: Credentials/v1
        properties:
          username: "hostname-only"
  - identity:
      type: OCIRegistry
      hostname: "ghcr.io"
      port: "`+port+`"
    credentials:
      - type: Credentials/v1
        properties:
          username: "with-port"
`)
			r.NoError(err, "config that loads on main must still load")

			typed, err := graph.Resolve(t.Context(), runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "port": port})
			r.NoError(err)
			dc, ok := typed.(*v1.DirectCredentials)
			r.True(ok)
			r.Equal("with-port", dc.Properties["username"])
		})
	}
}
