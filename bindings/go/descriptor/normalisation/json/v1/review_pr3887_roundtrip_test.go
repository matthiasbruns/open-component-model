package v1_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// TestReviewPR3887_R1_StorageRoundTripStable asserts that the normalised bytes
// of a descriptor do not change when it is stored and read back. The OCI
// repository encodes with yaml.Marshal (oci/spec/descriptor/encode.go) and
// decodes every media type, JSON included, with sigs.k8s.io/yaml.Unmarshal
// (oci/spec/descriptor/decode.go). Signing an in-memory descriptor and
// verifying the stored copy must produce the same digest.
func TestReviewPR3887_R1_StorageRoundTripStable(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"object-in-list-unsorted-keys", `[{"b":1,"a":2}]`},
		{"nested-dict-list-unsorted-keys", `{"z":[{"q":1,"p":2}],"a":"first"}`},
		{"float-trailing-zero", `1.0`},
		{"float-exponent", `{"n":1.0e+5}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			desc := &v2.Descriptor{
				Meta: v2.Meta{Version: "v2"},
				Component: v2.Component{
					ComponentMeta: v2.ComponentMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "acme.org/case",
							Version: "1.0.0",
							Labels:  []v2.Label{{Name: "x", Value: json.RawMessage(tt.value), Signing: true}},
						},
					},
					Provider:   `{"name":"ACME"}`,
					Resources:  []v2.Resource{},
					Sources:    []v2.Source{},
					References: []v2.Reference{},
				},
			}

			inMemory, err := descruntime.ConvertFromV2(desc)
			r.NoError(err)
			signed, err := normalisation.Normalise(inMemory, v1.Algorithm)
			r.NoError(err)

			stored, err := yaml.Marshal(desc)
			r.NoError(err)
			var fetched v2.Descriptor
			r.NoError(yaml.Unmarshal(stored, &fetched))
			fetchedRt, err := descruntime.ConvertFromV2(&fetched)
			r.NoError(err)
			verified, err := normalisation.Normalise(fetchedRt, v1.Algorithm)
			r.NoError(err)

			r.Equal(string(signed), string(verified), "normalised bytes change across the OCI store round trip")
		})
	}
}
