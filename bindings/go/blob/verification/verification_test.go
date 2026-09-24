package verification_test

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/blob/verification"
)

const verifyTestContent = "the content a digest was taken over"

type closableBlob struct {
	*inmemory.Blob
	closed bool
}

func (c *closableBlob) Close() error {
	c.closed = true
	return nil
}

func newVerifying(t *testing.T, content string, expected digest.Digest) blob.ReadOnlyBlob {
	t.Helper()
	r := require.New(t)
	b, err := verification.Wrap(inmemory.New(strings.NewReader(content)), expected)
	r.NoError(err)
	return b
}

func TestVerifyingBlob_MatchingContent(t *testing.T) {
	r := require.New(t)
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))

	rc, err := b.ReadCloser()
	r.NoError(err)

	read, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(verifyTestContent, string(read))
	r.NoError(rc.Close())

	dig, known := b.(blob.DigestAware).Digest()
	r.True(known)
	r.Equal(digest.FromString(verifyTestContent).String(), dig)
}

func TestVerifyingBlob_DigestReportsActualContent(t *testing.T) {
	r := require.New(t)
	promised := digest.FromString("what the descriptor promised")
	b := newVerifying(t, verifyTestContent, promised)

	dig, known := b.(blob.DigestAware).Digest()
	r.True(known)
	r.Equal(digest.FromString(verifyTestContent).String(), dig)
	r.NotEqual(promised.String(), dig)
}

func TestVerifyingBlob_TamperedContent(t *testing.T) {
	r := require.New(t)
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	rc, err := b.ReadCloser()
	r.NoError(err)

	_, err = io.ReadAll(rc)
	r.ErrorContains(err, "digest mismatch")
	r.ErrorContains(rc.Close(), "digest mismatch")
}

func TestVerifyingBlob_PartialReadFailsOnClose(t *testing.T) {
	r := require.New(t)
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))

	rc, err := b.ReadCloser()
	r.NoError(err)

	_, err = io.CopyN(io.Discard, rc, 4)
	r.NoError(err)

	r.ErrorContains(rc.Close(), "digest mismatch")
}

func TestVerifyingBlob_EachReaderVerifiesIndependently(t *testing.T) {
	r := require.New(t)
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))

	for range 2 {
		rc, err := b.ReadCloser()
		r.NoError(err)
		_, err = io.ReadAll(rc)
		r.NoError(err)
		r.NoError(rc.Close())
	}
}

func TestVerifyingBlob_ForwardsToUnderlyingBlob(t *testing.T) {
	r := require.New(t)
	inner := inmemory.New(strings.NewReader(verifyTestContent), inmemory.WithMediaType("application/x-tar"))
	closable := &closableBlob{Blob: inner}

	b, err := verification.Wrap(closable, digest.FromString(verifyTestContent))
	r.NoError(err)

	r.Equal(int64(len(verifyTestContent)), b.(blob.SizeAware).Size())

	mediaType, known := b.(blob.MediaTypeAware).MediaType()
	r.True(known)
	r.Equal("application/x-tar", mediaType)

	b.(blob.MediaTypeOverrideable).SetMediaType("application/octet-stream")
	mediaType, _ = b.(blob.MediaTypeAware).MediaType()
	r.Equal("application/octet-stream", mediaType)

	r.NoError(b.(io.Closer).Close())
	r.True(closable.closed, "wrapping must not hide the Close that releases the underlying resource")
}

func TestVerifyingBlob_RejectsUnusableExpectedDigest(t *testing.T) {
	r := require.New(t)
	for _, expected := range []digest.Digest{"", "not-a-digest", "sha256:tooshort"} {
		_, err := verification.Wrap(inmemory.New(strings.NewReader(verifyTestContent)), expected)
		r.Error(err, "expected digest %q must be rejected", expected)
	}
}

func TestVerifyingBlob_CopyReportsMismatch(t *testing.T) {
	r := require.New(t)
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	err := blob.Copy(io.Discard, b)
	r.ErrorContains(err, "digest mismatch")
}

func TestVerifyingBlob_CopyBlobToOSPathReportsMismatch(t *testing.T) {
	r := require.New(t)
	b := newVerifying(t, verifyTestContent, digest.FromString("what the descriptor promised"))

	err := filesystem.CopyBlobToOSPath(b, filepath.Join(t.TempDir(), "out"))
	r.ErrorContains(err, "digest mismatch")
}

func TestVerifyingBlob_VerifiesContentOfUnknownSize(t *testing.T) {
	r := require.New(t)
	b, err := verification.Wrap(plainBlob{content: verifyTestContent}, digest.FromString(verifyTestContent))
	r.NoError(err)
	r.Equal(blob.SizeUnknown, b.(blob.SizeAware).Size())

	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
}

func TestVerifyingBlob_RejectsTrailingContent(t *testing.T) {
	r := require.New(t)
	b, err := verification.Wrap(
		sizedBlob{plainBlob{content: verifyTestContent + " and more"}, int64(len(verifyTestContent))},
		digest.FromString(verifyTestContent),
	)
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.ReadAll(rc)
	r.ErrorContains(err, "digest mismatch")
	r.ErrorContains(rc.Close(), "digest mismatch")
}

func TestVerifyingBlob_UnreadContentCannotMatchEmptyDigest(t *testing.T) {
	r := require.New(t)
	b, err := verification.Wrap(inmemory.New(strings.NewReader(verifyTestContent)), digest.FromString(""))
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	r.ErrorContains(rc.Close(), "incomplete read for digest")
}

func TestVerifyingBlob_CopyKnownSize(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		size    int64
		err     string
	}{
		{name: "matching", content: verifyTestContent, size: int64(len(verifyTestContent))},
		{name: "empty", size: 0},
		{name: "trailing", content: verifyTestContent + " extra", size: int64(len(verifyTestContent)), err: "trailing content"},
		{name: "incomplete", content: verifyTestContent, size: int64(len(verifyTestContent)) + 1, err: "EOF"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			b, err := verification.Wrap(sizedBlob{plainBlob{content: tt.content}, tt.size}, digest.FromString(tt.content))
			r.NoError(err)
			err = blob.Copy(io.Discard, b)
			if tt.err != "" {
				r.ErrorContains(err, tt.err)
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestVerifyingBlob_ExactReadWithoutEOFFailsOnClose(t *testing.T) {
	r := require.New(t)
	b := newVerifying(t, verifyTestContent, digest.FromString(verifyTestContent))
	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.CopyN(io.Discard, rc, int64(len(verifyTestContent)))
	r.NoError(err)
	r.ErrorContains(rc.Close(), "incomplete read for digest")
}

// sizedBlob gives a plainBlob a size without giving it anything else.
type sizedBlob struct {
	plainBlob
	size int64
}

func (s sizedBlob) Size() int64 { return s.size }

// plainBlob implements nothing beyond ReadOnlyBlob.
type plainBlob struct {
	content string
}

func (p plainBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(p.content)), nil
}
