package constructor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha3"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func reviewPR4006Child(labelVersion string) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/child",
					Version: "1.0.0",
					Labels: []descriptor.Label{{
						Name:    "policy",
						Value:   json.RawMessage(`"strict"`),
						Signing: true,
						Version: labelVersion,
					}},
				},
			},
			Provider: descriptor.Provider{Name: "ocm.software"},
		},
	}
}

// A parent signed with the new default (v4alpha3) covers its children only
// through the component reference digests the constructor records. Those are
// still computed with v4alpha1, which drops label.version, so a child's signed
// label.version is not covered transitively.
func TestReviewPR4006_ReferenceDigestCoversChildLabelVersion(t *testing.T) {
	r := require.New(t)

	a3v1, err := normalisation.Normalise(reviewPR4006Child("v1"), v4alpha3.Algorithm)
	r.NoError(err)
	a3v2, err := normalisation.Normalise(reviewPR4006Child("v2"), v4alpha3.Algorithm)
	r.NoError(err)
	r.NotEqual(string(a3v1), string(a3v2), "control: v4alpha3 signs label.version")

	d1, err := calculateDigest(reviewPR4006Child("v1"))
	r.NoError(err)
	d2, err := calculateDigest(reviewPR4006Child("v2"))
	r.NoError(err)
	t.Logf("reference digest algorithm: %s", d1.NormalisationAlgorithm)
	r.NotEqual(d1.Value, d2.Value,
		"component reference digest (%s) must change when a child's signed label.version changes", d1.NormalisationAlgorithm)
}
