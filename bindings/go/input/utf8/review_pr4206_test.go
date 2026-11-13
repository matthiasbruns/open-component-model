package utf8_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/input/utf8"
	v1 "ocm.software/open-component-model/bindings/go/input/utf8/spec/v1"
)

// A constructor input must produce the same blob bytes, and so the same resource
// digest, as the release before the json/v2 migration.
func TestReviewPR4206_S3_JSONInputBytesUnchanged(t *testing.T) {
	in := []byte("{\"a\": \"<b>&c\", \"u\": \"\u2028\"}")
	for name, tc := range map[string]struct {
		spec v1.UTF8
		want string
	}{
		"json": {
			spec: v1.UTF8{JSON: in},
			want: `{"a":"\u003cb\u003e\u0026c","u":"\u2028"}`,
		},
		"formattedJson": {
			spec: v1.UTF8{FormattedJSON: in},
			want: "{\n  \"a\": \"\\u003cb\\u003e\\u0026c\",\n  \"u\": \"\\u2028\"\n}",
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			b, err := utf8.GetV1UTF8Blob(tc.spec)
			r.NoError(err)
			rc, err := b.ReadCloser()
			r.NoError(err)
			got, err := io.ReadAll(rc)
			r.NoError(err)
			r.NoError(rc.Close())
			r.Equal(tc.want, string(got))
		})
	}
}
