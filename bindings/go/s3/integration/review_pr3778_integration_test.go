package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/s3/repository"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	accessv2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

const (
	reviewMinIOImage  = "quay.io/minio/minio:RELEASE.2025-09-07T16-13-09Z"
	reviewMinIOUser   = "ocm-test"
	reviewMinIOSecret = "ocm-test-secret"
)

func reviewStartMinIO(t *testing.T, ctx context.Context) string {
	t.Helper()
	r := require.New(t)
	container, err := testcontainers.Run(ctx, reviewMinIOImage,
		testcontainers.WithExposedPorts("9000/tcp"),
		testcontainers.WithCmd("server", "/data"),
		testcontainers.WithEnv(map[string]string{"MINIO_ROOT_USER": reviewMinIOUser, "MINIO_ROOT_PASSWORD": reviewMinIOSecret}),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/minio/health/ready").WithPort("9000/tcp")),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })
	hostPort, err := container.PortEndpoint(ctx, "9000/tcp", "")
	r.NoError(err)
	return hostPort
}

// reviewResponse is what a store answered to one request of the probe.
type reviewResponse struct {
	status int
	header http.Header
}

// Test_Integration_ReviewPR3778_StoreProbe runs the PR's assumptions against real stores
// (RustFS and MinIO from local images):
//   - multipart objects uploaded with SHA-256, with 3 parts and with 1 part: the digest
//     must be the content's SHA-256 and the download must return the whole object;
//   - it logs what HEAD and part GETs carry: checksum type and value, Content-Range,
//     parts count (F1 and Q3 realism);
//   - a PutObject with a wrong x-amz-checksum-sha256: whether the store rejects it (Q2).
func Test_Integration_ReviewPR3778_StoreProbe(t *testing.T) {
	ctx := context.Background()
	stores := []struct {
		name, user, secret string
		start              func(*testing.T, context.Context) string
	}{
		{name: "rustfs", user: rustfsAccessKey, secret: rustfsSecretKey, start: startRustFS},
		{name: "minio", user: reviewMinIOUser, secret: reviewMinIOSecret, start: reviewStartMinIO},
	}
	for _, store := range stores {
		t.Run(store.name, func(t *testing.T) {
			hostPort := store.start(t, ctx)
			endpoint := "http://" + hostPort
			setup := newSetupClient(t, ctx, endpoint, store.user, store.secret)
			creds := &credv1.S3Credentials{Type: credv1.S3CredentialsVersionedType, AccessKeyID: store.user, SecretAccessKey: store.secret}
			tempDir := t.TempDir()
			fsConfig := &filesystemv1alpha1.Config{TempFolder: &tempDir}
			const bucket = "review-probe"
			createBucket(t, ctx, setup, bucket)

			const partSize = 5 * 1024 * 1024
			for _, tc := range []struct {
				name  string
				parts [][]byte
			}{
				{name: "multipart with 3 parts", parts: [][]byte{bytes.Repeat([]byte("a"), partSize), bytes.Repeat([]byte("b"), partSize), []byte("tail")}},
				{name: "multipart with 1 part", parts: [][]byte{[]byte("a single small part")}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := require.New(t)
					key := strings.ReplaceAll(tc.name, " ", "-")
					content := bytes.Join(tc.parts, nil)
					reviewMultipartUpload(t, ctx, setup, bucket, key, tc.parts)

					var mu sync.Mutex
					responses := map[string]reviewResponse{}
					client := &http.Client{Transport: recordingTransport(func(req *http.Request) (*http.Response, error) {
						resp, err := http.DefaultTransport.RoundTrip(req)
						if err == nil {
							id := req.Method
							if n := req.URL.Query().Get("partNumber"); n != "" {
								id += " partNumber=" + n
							}
							if rng := req.Header.Get("Range"); rng != "" {
								id += " Range=" + rng
							}
							mu.Lock()
							responses[id] = reviewResponse{status: resp.StatusCode, header: resp.Header.Clone()}
							mu.Unlock()
						}
						return resp, err
					})}
					repo := repository.NewResourceRepository(fsConfig, repository.WithHTTPClient(client))
					res := &descriptor.Resource{}
					res.Access = &accessv2.S3{Type: accessspec.V2VersionedType, Region: "us-east-1", BucketName: bucket, ObjectKey: key, Endpoint: endpoint, UsePathStyle: true}

					withDigest, err := repo.ProcessResourceDigest(ctx, res, creds)
					r.NoError(err)
					for id, resp := range responses {
						t.Logf("%s %s -> %d Content-Range=%q Content-Length=%q x-amz-mp-parts-count=%q ETag=%s x-amz-checksum-type=%q x-amz-checksum-sha256=%q",
							store.name, id, resp.status, resp.header.Get("Content-Range"), resp.header.Get("Content-Length"),
							resp.header.Get("x-amz-mp-parts-count"), resp.header.Get("ETag"),
							resp.header.Get("x-amz-checksum-type"), resp.header.Get("x-amz-checksum-sha256"))
					}
					r.Equal(godigest.SHA256.FromBytes(content).Encoded(), withDigest.Digest.Value, "the digest is the content's SHA-256")

					b, err := repo.DownloadResource(ctx, withDigest, creds)
					r.NoError(err)
					rc, err := b.ReadCloser()
					r.NoError(err)
					got, err := io.ReadAll(rc)
					r.NoError(err)
					r.NoError(rc.Close())
					r.True(bytes.Equal(content, got), "the download returns the whole object")
				})
			}

			t.Run("put with a wrong SHA-256 checksum", func(t *testing.T) {
				wrong := sha256.Sum256([]byte("not the body"))
				_, err := setup.PutObject(ctx, &s3.PutObjectInput{
					Bucket:         new(bucket),
					Key:            new("wrong-checksum"),
					Body:           bytes.NewReader([]byte("the body")),
					ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(wrong[:])),
				})
				t.Logf("%s PutObject with a wrong x-amz-checksum-sha256: err=%v", store.name, err)
				require.Error(t, err, "the store must reject a checksum that does not match the body")
			})
		})
	}
}

func reviewMultipartUpload(t *testing.T, ctx context.Context, client *s3.Client, bucket, key string, parts [][]byte) {
	t.Helper()
	r := require.New(t)
	upload, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: new(bucket), Key: new(key), ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	})
	r.NoError(err)
	var completed []types.CompletedPart
	for i, part := range parts {
		number := int32(i + 1)
		out, err := client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: new(bucket), Key: new(key), UploadId: upload.UploadId, PartNumber: &number,
			Body: bytes.NewReader(part), ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		})
		r.NoError(err)
		completed = append(completed, types.CompletedPart{ETag: out.ETag, PartNumber: &number, ChecksumSHA256: out.ChecksumSHA256})
	}
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: new(bucket), Key: new(key), UploadId: upload.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	r.NoError(err)
}
