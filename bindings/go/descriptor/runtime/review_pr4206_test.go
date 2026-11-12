package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptorRuntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// TestReviewPR4206_F2_ProviderRoundTripIsByteStable pins round-trip stability of the provider
// field of an already-stored component descriptor.
//
// The provider is stored as a JSON document embedded in a JSON string. Reading a descriptor
// parses that string (ConvertFromV2Provider) and writing one regenerates it
// (ConvertToV2Provider) with its own json.Marshal call. That call is the only place where
// encoding/json/v2 output reaches the bytes of a stored descriptor: the descriptor struct
// itself is marshaled by sigs.k8s.io/yaml, i.e. encoding/json v1
// (oci/spec/descriptor/encode.go).
//
// v1 escaped <, > and & as <, > and &; v2 does not. So a descriptor written by
// any earlier release whose provider carries those characters - a company name with "&", a
// contact address in angle brackets, a URL with query parameters - is re-encoded to different
// bytes, and therefore a different blob digest, by a no-op read/write cycle.
//
// Signature verification is unaffected: normalisation re-parses the provider, so the escaping
// cancels out. This is about the stored bytes, which the PR describes as unchanged.
func TestReviewPR4206_F2_ProviderRoundTripIsByteStable(t *testing.T) {
	for _, tt := range []struct {
		name   string
		stored string
	}{
		{
			name:   "label name with HTML-special characters, as written by earlier releases",
			stored: `{"name":"acme","labels":[{"name":"l\u003cx\u003e\u0026y","value":"v","version":"v1"}]}`,
		},
		{
			name:   "label value with an ampersand and angle brackets",
			stored: `{"name":"acme.example.com","labels":[{"name":"contact","value":"Smith \u0026 Sons \u003clegal@acme.example.com\u003e","version":"v1"}]}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Read a descriptor that is already stored in a registry.
			provider, err := descriptorRuntime.ConvertFromV2Provider(tt.stored)
			r.NoError(err)

			// Write it back out unchanged.
			roundTripped, err := descriptorRuntime.ConvertToV2Provider(provider)
			r.NoError(err)

			// A read/write cycle that changes nothing must not change the bytes, or the
			// descriptor blob digest moves on every transfer and re-upload.
			r.Equal(tt.stored, roundTripped,
				"re-encoding an unmodified stored descriptor changed its provider bytes")
		})
	}
}
