// Package verification provides streaming verification of blob content against an
// independently supplied digest.
package verification

import (
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

type verifyingBlob struct {
	base     blob.ReadOnlyBlob
	expected digest.Digest
}

var (
	_ blob.ReadOnlyBlob          = (*verifyingBlob)(nil)
	_ blob.SizeAware             = (*verifyingBlob)(nil)
	_ blob.DigestAware           = (*verifyingBlob)(nil)
	_ blob.MediaTypeAware        = (*verifyingBlob)(nil)
	_ blob.MediaTypeOverrideable = (*verifyingBlob)(nil)
	_ io.Closer                  = (*verifyingBlob)(nil)
)

// Wrap returns base wrapped so that every reader independently verifies its content
// against expected. It fails if expected is invalid or its algorithm is unavailable.
// Readers must reach EOF; closing an incomplete reader returns an error even if the
// bytes read match expected. Digest mismatches are reported by both Read and Close.
//
// Verification is streaming: callers must discard any output on failure. Metadata
// and blob-level Close are forwarded to base; Digest reports the underlying blob's
// digest, not expected.
func Wrap(base blob.ReadOnlyBlob, expected digest.Digest) (blob.ReadOnlyBlob, error) {
	if err := expected.Validate(); err != nil {
		return nil, fmt.Errorf("invalid expected digest %q: %w", expected, err)
	}
	if !expected.Algorithm().Available() {
		return nil, fmt.Errorf("digest algorithm %q of expected digest %q is not available", expected.Algorithm(), expected)
	}

	return &verifyingBlob{base: base, expected: expected}, nil
}

// ReadCloser returns a reader over the content that verifies it against the
// expected digest. A complete read reports mismatches; Close also reports
// mismatches and incomplete reads.
func (b *verifyingBlob) ReadCloser() (io.ReadCloser, error) {
	rc, err := b.base.ReadCloser()
	if err != nil {
		return nil, err
	}
	return &verifyingReadCloser{
		base:     rc,
		digester: b.expected.Algorithm().Digester(),
		expected: b.expected,
	}, nil
}

// Digest forwards to the underlying blob, so it reports what the content IS, not
// what it is expected to be.
func (b *verifyingBlob) Digest() (string, bool) {
	if digestAware, ok := b.base.(blob.DigestAware); ok {
		return digestAware.Digest()
	}
	return "", false
}

// Size forwards to the underlying blob, or reports SizeUnknown if it does not know
// it. Nothing here needs the size: the hash covers whatever is read.
func (b *verifyingBlob) Size() int64 {
	if sizeAware, ok := b.base.(blob.SizeAware); ok {
		return sizeAware.Size()
	}
	return blob.SizeUnknown
}

// MediaType returns the media type of the underlying blob if it has one.
func (b *verifyingBlob) MediaType() (string, bool) {
	if mediaTypeAware, ok := b.base.(blob.MediaTypeAware); ok {
		return mediaTypeAware.MediaType()
	}
	return "", false
}

// SetMediaType forwards to the underlying blob and is a no-op if it does not
// support overriding its media type.
func (b *verifyingBlob) SetMediaType(mediaType string) {
	if overrideable, ok := b.base.(blob.MediaTypeOverrideable); ok {
		overrideable.SetMediaType(mediaType)
	}
}

// Close forwards to the underlying blob so that wrapping does not leak the
// resources it owns, such as a temporary file. It is a no-op if the underlying
// blob is not an io.Closer.
func (b *verifyingBlob) Close() error {
	if closer, ok := b.base.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// verifyingReadCloser hashes everything read through it and compares the result
// against the expected digest.
type verifyingReadCloser struct {
	base     io.ReadCloser
	digester digest.Digester
	expected digest.Digest
	eof      bool
}

// Read reports mismatches at EOF so consumers that read the entire stream do
// not depend on checking Close to detect corrupted content.
func (v *verifyingReadCloser) Read(p []byte) (int, error) {
	n, err := v.base.Read(p)
	if n > 0 {
		if _, writeErr := v.digester.Hash().Write(p[:n]); writeErr != nil {
			return n, writeErr
		}
	}
	if errors.Is(err, io.EOF) {
		v.eof = true
		if mismatch := v.verify(); mismatch != nil {
			return n, mismatch
		}
	}
	return n, err
}

// Close closes the underlying reader and reports a mismatch, which includes the
// content having been read only in part. The reader is closed either way, so a
// failed verification does not leak what it was reading from.
func (v *verifyingReadCloser) Close() error {
	return errors.Join(v.verify(), v.base.Close())
}

// verify refuses content that has not reached EOF before comparing digests.
func (v *verifyingReadCloser) verify() error {
	if !v.eof {
		return fmt.Errorf("digest mismatch: incomplete read for digest %s", v.expected)
	}
	if actual := v.digester.Digest(); actual != v.expected {
		return fmt.Errorf("digest mismatch: expected %s, got %s", v.expected, actual)
	}
	return nil
}
