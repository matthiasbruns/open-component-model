package signing

import (
	"crypto/fips140"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Unlike the existing mode-injection test, this uses the process's real mode.
func TestReviewFIPSEvidenceDigestRuntimeMode(t *testing.T) {
	r := require.New(t)
	cd := &descruntime.Component{References: []descruntime.Reference{{
		Digest: descruntime.Digest{HashAlgorithm: "MD5", NormalisationAlgorithm: "genericBlobDigest/v1", Value: "abcd"},
	}}}
	err := IsSafelyDigestible(cd)
	if fips140.Enforced() {
		r.ErrorIs(err, ErrUnsupportedDigestHash)
	} else {
		r.NoError(err)
	}
	t.Logf("enabled=%v enforced=%v MD5 reference IsSafelyDigestible=%v", fips140.Enabled(), fips140.Enforced(), err)
}
