package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// A Raw read from a YAML document must keep a case-variant "Type" like encoding/json v1 did.
func TestReviewPR4206_S4_RawTypeCaseVariant(t *testing.T) {
	r := require.New(t)
	var holder struct {
		Configurations []*runtime.Raw `json:"configurations"`
	}
	r.NoError(yaml.Unmarshal([]byte("configurations:\n- Type: credentials.config.ocm.software\n"), &holder))
	r.Len(holder.Configurations, 1)
	r.Equal("credentials.config.ocm.software", holder.Configurations[0].Type.String())
}
