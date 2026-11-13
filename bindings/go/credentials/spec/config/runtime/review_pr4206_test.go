package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
)

// An .ocmconfig is read through sigs.k8s.io/yaml (encoding/json v1 semantics), so
// member names match case-insensitively. Consumers must load the same way.
func TestReviewPR4206_S2_ConsumerCaseVariantMembers(t *testing.T) {
	for name, cfg := range map[string]string{
		"canonical": `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identities:
    - type: OCIRepository
      hostname: ghcr.io
    credentials:
    - type: Credentials/v1
      properties:
        username: u
`,
		"case variant": `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - Identities:
    - type: OCIRepository
      hostname: ghcr.io
    Credentials:
    - type: Credentials/v1
      properties:
        username: u
`,
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			var generic genericv1.Config
			r.NoError(yaml.Unmarshal([]byte(cfg), &generic))

			got, err := LookupCredentialConfig(&generic)
			r.NoError(err)
			r.NotNil(got)
			r.Len(got.Consumers, 1)
			r.Len(got.Consumers[0].Identities, 1, "identities dropped")
			r.Len(got.Consumers[0].Credentials, 1, "credentials dropped")
		})
	}
}
