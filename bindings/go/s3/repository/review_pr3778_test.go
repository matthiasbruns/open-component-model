package repository

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

// TestReviewPR3778_F1_DigestOfTruncatedMultipartObject follows F1 through the
// repository: a store that answers partNumber=1 without Content-Range while reporting
// three parts. The digest must be the digest of the whole object, and a later download
// must return the whole object.
func TestReviewPR3778_F1_DigestOfTruncatedMultipartObject(t *testing.T) {
	r := require.New(t)
	parts := [][]byte{[]byte("first part "), []byte("second part "), []byte("tail")}
	content := bytes.Join(parts, nil)

	var gets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header()["Content-Type"] = nil
		w.Header().Set("ETag", fmt.Sprintf(`"0123456789abcdef-%d"`, len(parts)))
		if req.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			return
		}
		gets++
		if req.URL.Query().Get("partNumber") == "1" {
			w.Header().Set("x-amz-mp-parts-count", strconv.Itoa(len(parts)))
			w.Header().Set("Content-Length", strconv.Itoa(len(parts[0])))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(parts[0])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	temp := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &temp})
	spec := &v2.S3{BucketName: "b", ObjectKey: "k", Endpoint: srv.URL, UsePathStyle: true}

	res, err := repo.ProcessResourceDigest(t.Context(), s3Resource(spec), fakeCredentials())
	r.NoError(err)
	t.Logf("recorded digest %s:%s (whole object sha256:%s, part 1 sha256:%s)",
		res.Digest.HashAlgorithm, res.Digest.Value, godigest.FromBytes(content).Encoded(), godigest.FromBytes(parts[0]).Encoded())

	b, err := repo.DownloadResource(t.Context(), res, fakeCredentials())
	r.NoError(err)
	rc, err := b.ReadCloser()
	r.NoError(err)
	downloaded, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	t.Logf("later download: %d bytes, read err %v, close err %v", len(downloaded), readErr, closeErr)

	r.Equal(godigest.FromBytes(content).Encoded(), res.Digest.Value, "the digest must cover the whole object, not part 1")
	r.Equal(string(content), string(downloaded), "a later download must return the whole object")
}

// TestReviewPR3778_Q2_StoreChecksumIsTrustedUntilRead characterises the trust model: a
// store whose advertised SHA-256 does not match its bytes. The fast path records the
// advertised value from one HEAD, and the mismatch surfaces only on a later read. The
// test passes when that is the behaviour.
func TestReviewPR3778_Q2_StoreChecksumIsTrustedUntilRead(t *testing.T) {
	r := require.New(t)
	content := []byte("the bytes the store really holds")
	claimed := sha256.Sum256([]byte("bytes the uploader claimed"))

	srv := newFakeS3(t, content, "v-1")
	srv.header = http.Header{}
	srv.header.Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(claimed[:]))
	srv.header.Set("x-amz-checksum-type", "FULL_OBJECT")

	temp := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &temp})
	res, err := repo.ProcessResourceDigest(t.Context(), s3Resource(servedBy(srv, &v2.S3{BucketName: "b", ObjectKey: "k"})), fakeCredentials())
	r.NoError(err, "digest processing does not read the bytes")
	r.Equal(fmt.Sprintf("%x", claimed), res.Digest.Value, "the advertised checksum becomes the descriptor digest")
	r.Len(srv.recorded(), 1)
	r.Equal(http.MethodHead, srv.recorded()[0].method)

	// The SDK validates a complete (200) GET against the same advertised checksum, so the
	// mismatch can already fail the download; otherwise the verifying read catches it.
	b, err := repo.DownloadResource(t.Context(), res, fakeCredentials())
	if err != nil {
		t.Logf("download error: %v", err)
		return
	}
	rc, err := b.ReadCloser()
	r.NoError(err)
	_, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	r.Error(errorsJoin(readErr, closeErr), "the mismatch must surface on the first read")
	t.Logf("read error: %v / close error: %v", readErr, closeErr)
}

// TestReviewPR3778_Q1_FastPathIgnoresMaxDownloadSize characterises the missing switch:
// with every repository option at its strictest, an object above the size limit is
// still digested from the HEAD alone, and no option keeps the HEAD from being sent.
func TestReviewPR3778_Q1_FastPathIgnoresMaxDownloadSize(t *testing.T) {
	r := require.New(t)
	content := []byte("an object larger than the configured limit")
	sum := sha512.Sum512(content)

	srv := newFakeS3(t, content, "v-1")
	srv.header = http.Header{}
	srv.header.Set("x-amz-checksum-sha512", base64.StdEncoding.EncodeToString(sum[:]))

	temp := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &temp}, WithMaxDownloadSize(1))
	res, err := repo.ProcessResourceDigest(t.Context(), s3Resource(servedBy(srv, &v2.S3{BucketName: "b", ObjectKey: "k"})), fakeCredentials())
	r.NoError(err, "the size limit does not apply on the fast path")
	r.Equal("SHA-512", res.Digest.HashAlgorithm, "a store keeping only SHA-512 makes the descriptor SHA-512")
	r.Len(srv.recorded(), 1)
	r.Equal(http.MethodHead, srv.recorded()[0].method)

	_, err = repo.DownloadResource(t.Context(), res, fakeCredentials())
	r.ErrorContains(err, "exceeds maximum allowed size", "the same resource cannot be downloaded under the same limit")
}

// TestReviewPR3778_N1_PrefixSpellingsMatchVerifyDownload: ProcessResourceDigest should
// accept every digest-value spelling the shared VerifyDownload accepts, so that a digest
// that verifies on download is also accepted at construction.
func TestReviewPR3778_N1_PrefixSpellingsMatchVerifyDownload(t *testing.T) {
	content := []byte("digest me")
	sha256Hex := godigest.SHA256.FromBytes(content).Encoded()

	tests := []struct {
		name   string
		digest *descriptor.Digest
	}{
		{name: "SHA-256 with a sha-256: prefix", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: "sha-256:" + sha256Hex}},
		{name: "SHA-256 with a SHA-256: prefix", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: "SHA-256:" + sha256Hex}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeS3(t, content, "")

			temp := t.TempDir()
			repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &temp})

			withDigest := s3Resource(servedBy(srv, &v2.S3{BucketName: "b", ObjectKey: "k"}))
			withDigest.Digest = tt.digest
			b, err := repo.DownloadResource(t.Context(), withDigest, fakeCredentials())
			r.NoError(err)
			rc, err := b.ReadCloser()
			r.NoError(err)
			_, err = io.ReadAll(rc)
			r.NoError(err, "VerifyDownload accepts this spelling")
			r.NoError(rc.Close())

			_, err = repo.ProcessResourceDigest(t.Context(), withDigest, fakeCredentials())
			r.NoError(err, "ProcessResourceDigest should accept the same spelling")
		})
	}
}

func errorsJoin(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
