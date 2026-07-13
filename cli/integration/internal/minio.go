package internal

import (
	"bytes"
	"context"
	"net"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"
)

// MinIOImage is the pinned MinIO image used for S3-compatible integration tests.
const MinIOImage = "minio/minio:RELEASE.2024-01-16T16-07-38Z"

// MinIO is a running MinIO container plus a preconfigured admin S3 client used to
// seed buckets and objects for tests.
type MinIO struct {
	Endpoint string // http://host:port
	Host     string
	Port     string
	User     string
	Password string
	client   *s3.Client
}

// CreateMinIO starts a MinIO container and returns its connection details. The
// container is terminated on test cleanup.
func CreateMinIO(t *testing.T) *MinIO {
	t.Helper()
	r := require.New(t)

	container, err := minio.Run(context.Background(), MinIOImage)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })

	hostPort, err := container.ConnectionString(context.Background())
	r.NoError(err)
	host, port, err := net.SplitHostPort(hostPort)
	r.NoError(err)
	endpoint := "http://" + hostPort

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(container.Username, container.Password, "")),
	)
	r.NoError(err)

	return &MinIO{
		Endpoint: endpoint,
		Host:     host,
		Port:     port,
		User:     container.Username,
		Password: container.Password,
		client: s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}),
	}
}

// CreateBucket creates a bucket in the running MinIO.
func (m *MinIO) CreateBucket(t *testing.T, bucket string) {
	t.Helper()
	_, err := m.client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
}

// PutObject seeds an object into the given bucket.
func (m *MinIO) PutObject(t *testing.T, bucket, key string, content []byte) {
	t.Helper()
	_, err := m.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	})
	require.NoError(t, err)
}
