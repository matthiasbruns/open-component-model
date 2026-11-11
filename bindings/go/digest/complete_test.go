package digest_test

import (
	"crypto"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmdigest "ocm.software/open-component-model/bindings/go/digest"
)

func TestComplete(t *testing.T) {
	const (
		value = "4f1c0000"
		other = "9a070000"
		blob  = descriptor.GenericBlobDigestV1
	)
	computed := ocmdigest.Computed{Hash: crypto.SHA256, Value: value}
	artifactRule := ocmdigest.Rule{Normalisation: descriptor.OCIArtifactDigestV1, Accepted: []string{blob}}

	complete := &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: blob, Value: value}

	for _, tc := range []struct {
		name     string
		authored *descriptor.Digest
		computed *ocmdigest.Computed
		rule     ocmdigest.Rule
		want     *descriptor.Digest
		wantErr  string
	}{
		{
			name: "nothing written is taken from what was resolved",
			want: complete,
		},
		{
			name:     "an empty digest is taken from what was resolved",
			authored: &descriptor.Digest{},
			want:     complete,
		},
		{
			name:     "a value alone is completed",
			authored: &descriptor.Digest{Value: value},
			want:     complete,
		},
		{
			name:     "a hash name alone is completed",
			authored: &descriptor.Digest{HashAlgorithm: "sha256"},
			want:     complete,
		},
		{
			name:     "a spelling is canonicalised",
			authored: &descriptor.Digest{HashAlgorithm: "sha-256", NormalisationAlgorithm: "GenericBlobDigest/v1", Value: "4F1C0000"},
			want:     complete,
		},
		{
			name:     "a value carrying its algorithm is accepted",
			authored: &descriptor.Digest{Value: "sha256:" + value},
			want:     complete,
		},
		{
			name:     "a value carrying another algorithm is a conflict",
			authored: &descriptor.Digest{Value: "sha512:" + value},
			wantErr:  "digest value mismatch",
		},
		{
			name:     "another value is a conflict",
			authored: &descriptor.Digest{Value: other},
			wantErr:  "digest value mismatch: expected " + other + ", got " + value,
		},
		{
			name:     "another hash algorithm is a conflict",
			authored: &descriptor.Digest{HashAlgorithm: "SHA-512", Value: value},
			wantErr:  "hash algorithm mismatch: expected SHA-512, got SHA-256",
		},
		{
			name:     "an unsupported hash algorithm is rejected",
			authored: &descriptor.Digest{HashAlgorithm: "MD5", Value: value},
			wantErr:  "unsupported hash algorithm",
		},
		{
			name:     "another normalisation is a conflict",
			authored: &descriptor.Digest{NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: value},
			wantErr:  "normalisation algorithm mismatch: expected " + blob + ", got " + descriptor.OCIArtifactDigestV1,
		},
		{
			name:     "a label the rule also describes is accepted and canonicalised",
			authored: &descriptor.Digest{NormalisationAlgorithm: blob, Value: value},
			rule:     artifactRule,
			want:     &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: value},
		},
		{
			name:     "an exclusion marker is kept and nothing is resolved",
			authored: descriptor.NewExcludeFromSignatureDigest(),
			computed: &ocmdigest.Computed{},
			want:     descriptor.NewExcludeFromSignatureDigest(),
		},
		{
			name:     "an exclusion marker by normalisation alone is kept",
			authored: &descriptor.Digest{NormalisationAlgorithm: "Exclude-From-Signature"},
			computed: &ocmdigest.Computed{},
			want:     &descriptor.Digest{NormalisationAlgorithm: "Exclude-From-Signature"},
		},
		{
			name:     "a resolved digest of an unsupported hash cannot be recorded",
			computed: &ocmdigest.Computed{Hash: crypto.MD5, Value: value},
			wantErr:  "unsupported hash algorithm",
		},
		{
			name:     "a resolved digest without a value cannot be recorded",
			computed: &ocmdigest.Computed{Hash: crypto.SHA256},
			wantErr:  "computed digest has no value",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			in := computed
			if tc.computed != nil {
				in = *tc.computed
			}
			rule := tc.rule
			if rule.Normalisation == "" {
				rule = ocmdigest.BlobRule
			}

			got, err := ocmdigest.Complete(tc.authored, in, rule)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				r.Nil(got)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
			if tc.authored != nil {
				r.NotSame(tc.authored, got, "the authored digest is never aliased")
			}
		})
	}
}
