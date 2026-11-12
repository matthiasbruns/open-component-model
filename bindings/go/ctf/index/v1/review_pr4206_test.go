package v1

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReviewPR4206_F3_ArtifactIndexStaysReadable pins that an artifact-index.json which earlier
// releases could read still opens.
//
// DecodeIndex was json.NewDecoder(...).DisallowUnknownFields().Decode(...) on encoding/json v1,
// which matched member names case-insensitively: a member spelled "Repository" matched the
// known field and the index loaded. Under encoding/json/v2 that member is case-sensitively
// unknown, and RejectUnknownMembers(true) makes it fatal - so the whole index fails to decode
// and the CTF cannot be opened at all.
//
// UnmarshalRead additionally consumes the reader to EOF and rejects duplicate members, both of
// which Decoder.Decode tolerated.
//
// Legacy OCM writes canonical casing, so the trigger is a hand-edited or third-party-generated
// index. The failure mode - an archive that was readable becoming unopenable - is what makes
// this worth pinning rather than the likelihood.
func TestReviewPR4206_F3_ArtifactIndexStaysReadable(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
	}{
		{
			name: "canonical casing",
			raw:  `{"schemaVersion":1,"artifacts":[{"repository":"r","tag":"t","digest":"sha256:aa"}]}`,
		},
		{
			name: "artifact member casing accepted by encoding/json v1",
			raw:  `{"schemaVersion":1,"artifacts":[{"Repository":"r","tag":"t","digest":"sha256:aa"}]}`,
		},
		{
			name: "top-level member casing accepted by encoding/json v1",
			raw:  `{"SchemaVersion":1,"artifacts":[{"repository":"r","tag":"t","digest":"sha256:aa"}]}`,
		},
		{
			name: "trailing data tolerated by Decoder.Decode",
			raw:  `{"schemaVersion":1,"artifacts":[{"repository":"r","tag":"t","digest":"sha256:aa"}]}{}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			idx, err := DecodeIndex(bytes.NewReader([]byte(tt.raw)))
			r.NoError(err, "an index earlier releases could read no longer decodes, so the CTF cannot be opened")
			r.NotNil(idx)
		})
	}
}
