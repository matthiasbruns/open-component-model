package download

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// UploadRequest describes a single S3 object upload. The client-shaping fields
// (region, endpoint, path-style, TLS) mirror [Request].
type UploadRequest struct {
	Region                string
	BucketName            string
	ObjectKey             string
	Endpoint              string
	UsePathStyle          bool
	InsecureSkipTLSVerify bool
}

// UploadResult carries the resulting object version, set when the bucket is versioned.
type UploadResult struct {
	VersionID string
}

// Upload writes body to the object described by req and returns the resulting
// version id (empty when the bucket is not versioned). Credentials follow the same
// resolution as [Download]: static credentials when provided, otherwise the AWS
// default chain.
func Upload(ctx context.Context, req UploadRequest, body io.Reader, creds runtime.Typed) (*UploadResult, error) {
	if req.BucketName == "" {
		return nil, fmt.Errorf("bucketName is required")
	}
	if req.ObjectKey == "" {
		return nil, fmt.Errorf("objectKey is required")
	}

	client, err := newClient(ctx, Request{
		Region:                req.Region,
		Endpoint:              req.Endpoint,
		UsePathStyle:          req.UsePathStyle,
		InsecureSkipTLSVerify: req.InsecureSkipTLSVerify,
	}, creds)
	if err != nil {
		return nil, err
	}

	// PutObject needs a seekable body to compute length/checksum without chunked
	// signing; buffer the content. Multipart upload for very large objects is a
	// future improvement (see the S3 design notes).
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("error reading upload body: %w", err)
	}

	out, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(req.BucketName),
		Key:    aws.String(req.ObjectKey),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return nil, fmt.Errorf("error putting s3 object %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}

	return &UploadResult{VersionID: aws.ToString(out.VersionId)}, nil
}
