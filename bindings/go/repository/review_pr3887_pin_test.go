package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

func TestReviewPR3887_VerifyOnlyPin(t *testing.T) {
	for _, algorithm := range []string{"jsonNormalisation/v4alpha1", "jsonNormalisation/v1"} {
		t.Run(algorithm, func(t *testing.T) {
			r := require.New(t)
			desc := &descriptor.Descriptor{Meta: descriptor.Meta{Version: "v2"}, Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "acme.org/review", Version: "1.0.0"}},
				Provider:      descriptor.Provider{Name: "ACME"},
			}}
			norm, err := normalisation.Normalise(desc, algorithm)
			r.NoError(err)
			hash := sha256.Sum256(norm)
			pin := &descriptor.Digest{NormalisationAlgorithm: algorithm, HashAlgorithm: "SHA-256", Value: hex.EncodeToString(hash[:])}
			r.NoError(signing.VerifyDigestMatchesDescriptor(t.Context(), desc, descriptor.Signature{Digest: *pin}, slog.Default()))
			r.NoError(verifyNormalizedPin(t.Context(), desc, pin), "a matching pin is a read-only verification operation")
		})
	}
}
