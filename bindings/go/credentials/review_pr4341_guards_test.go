package credentials

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The tests in this file pass on the PR head. They pin two ranking rules that no test in
// the PR covers: removing either rule from synced_dag.go leaves the PR's suite green.

// TestReviewPR4341_G1_CredentialSupplierBeatsDependencyOnlyVertex pins rule 1 of
// matchAnyNode: a vertex without credentials and without a resolution edge must not win
// against a consumer with credentials, even when its identity is more specific.
func TestReviewPR4341_G1_CredentialSupplierBeatsDependencyOnlyVertex(t *testing.T) {
	r := require.New(t)
	dag := newSyncedDag()

	consumer := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io"}
	dependencyOnly := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/*"}
	r.NoError(dag.addIdentity(consumer))
	r.NoError(dag.addIdentity(dependencyOnly))
	dag.setCredentials(consumer.String(), &v1.DirectCredentials{Properties: map[string]string{"username": "consumer"}})

	lookup := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/repo"}
	r.True(lookup.Match(consumer))
	r.True(lookup.Match(dependencyOnly))

	for range 50 {
		vertex, err := dag.matchAnyNode(lookup)
		r.NoError(err)
		r.Equal(consumer.String(), vertex.ID)
	}
}

// TestReviewPR4341_G2_EmptyPathDoesNotCountAsScope pins the empty-path exception of
// scopeAttributeCount: an entry with path "" matches every path and must lose against a
// path-scoped entry.
func TestReviewPR4341_G2_EmptyPathDoesNotCountAsScope(t *testing.T) {
	r := require.New(t)
	dag := newSyncedDag()

	emptyPath := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": ""}
	scoped := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/*"}
	r.NoError(dag.addIdentity(emptyPath))
	r.NoError(dag.addIdentity(scoped))

	lookup := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/repo"}
	r.True(lookup.Match(emptyPath))
	r.True(lookup.Match(scoped))

	for range 50 {
		vertex, err := dag.matchAnyNode(lookup)
		r.NoError(err)
		r.Equal(scoped.String(), vertex.ID)
	}
}
