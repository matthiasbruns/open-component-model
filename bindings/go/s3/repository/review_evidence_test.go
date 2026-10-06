package repository

import (
	"crypto/sha512"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

func TestReview_SHA512Transfer(t *testing.T) {
	r := require.New(t)
	content := []byte("digest me")
	sum := sha512.Sum512(content)
	srv := newFakeS3(t, content, "v-1")
	srv.header = http.Header{}
	srv.header.Set("x-amz-checksum-sha512", base64.StdEncoding.EncodeToString(sum[:]))
	srv.header.Set("x-amz-checksum-type", "FULL_OBJECT")
	temp := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &temp})
	res, err := repo.ProcessResourceDigest(t.Context(), s3Resource(servedBy(srv, &v2.S3{BucketName: "b", ObjectKey: "k"})), fakeCredentials())
	r.NoError(err)
	r.Equal("SHA-512", res.Digest.HashAlgorithm)
	b, err := repo.DownloadResource(t.Context(), res, fakeCredentials())
	r.NoError(err)
	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	target, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	_, err = target.AddLocalResource(t.Context(), "ocm.software/review", "1.0.0", res, b)
	r.NoError(err, "S3 resource must transfer by value into OCI/CTF without changing its digest")
}

func TestReview_ConflictingDigestPrefix(t *testing.T) {
	r := require.New(t)
	content := []byte("digest me")
	srv := newFakeS3(t, content, "v-1")
	temp := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &temp})
	res := s3Resource(servedBy(srv, &v2.S3{BucketName: "b", ObjectKey: "k"}))
	res.Digest = &descriptor.Digest{HashAlgorithm: "SHA-256", Value: "sha512:" + godigest.SHA256.FromBytes(content).Encoded()}
	_, err := repo.ProcessResourceDigest(t.Context(), res, fakeCredentials())
	r.Error(err, "a digest value carrying a conflicting algorithm must be rejected")
}
