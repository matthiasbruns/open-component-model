package resource_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsconfig "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// A host-wide entry plus a path-scoped entry on the same registry is a common setup
// (default credentials for the registry, an override for one project). With the
// repository now in the resource identity, both entries match an image in that
// project, and the same lookup must still return the same credentials every time.
func TestReviewPR4396_F1_OverlappingEntriesResolveDeterministically(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	cfgYAML := `
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry/v1
            hostname: ghcr.io
        credentials:
          - type: Credentials/v1
            properties:
              username: host-wide
              password: host-wide
      - identities:
          - type: OCIRegistry/v1
            hostname: ghcr.io
            path: project1/**
        credentials:
          - type: Credentials/v1
            properties:
              username: project1
              password: project1
`
	var cfg genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(cfgYAML), &cfg))
	credCfg, err := credentialsconfig.LookupCredentialConfig(&cfg)
	r.NoError(err)
	identityScheme := runtime.NewScheme()
	ociidentityv1.MustRegisterIdentityType(identityScheme)
	graph, err := credentials.ToGraph(ctx, credCfg, credentials.Options{ConsumerIdentityTypeScheme: identityScheme})
	r.NoError(err)

	identity, err := resource.NewResourceRepository(nil).GetResourceCredentialConsumerIdentity(ctx, &descriptor.Resource{
		Access: &accessv1.OCIImage{
			Type:           runtime.NewVersionedType(accessv1.OCIImageType, accessv1.Version),
			ImageReference: "ghcr.io/project1/app:1.0.0",
		},
	})
	r.NoError(err)

	seen := map[string]int{}
	for range 200 {
		creds, err := graph.Resolve(ctx, identity)
		r.NoError(err)
		direct, ok := creds.(*v1.DirectCredentials)
		r.True(ok, "unexpected credential type %T", creds)
		seen[direct.Properties["username"]]++
	}
	r.Len(seen, 1, "the same lookup %q returned different credentials across calls: %v", identity.String(), seen)
}
