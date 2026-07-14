// Package coordinate implements the technology-independent coordinate mapping for the
// S3 access type: it reduces an S3 access to a [descriptor.Coordinate] and expands any
// coordinate (from S3 or any other technology) into an S3 access placed in a configured
// target location.
package coordinate

import (
	"fmt"
	"path"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

// Config describes where FromCoordinate places content in an S3 environment. It is the
// "where" (environment-specific); the coordinate is the "what" (technology-independent).
type Config struct {
	Bucket       string
	KeyPrefix    string
	Region       string
	Endpoint     string
	UsePathStyle bool
}

// Coordinator maps between S3 accesses and neutral coordinates. It is configured with a
// target [Config] used by FromCoordinate; ToCoordinate ignores it.
type Coordinator struct {
	target Config
}

var _ repository.Coordinator = (*Coordinator)(nil)

// New returns a Coordinator that places content into the given target location.
// For ToCoordinate-only use, a zero Config is fine.
func New(target Config) *Coordinator {
	return &Coordinator{target: target}
}

// ToCoordinate reduces an S3 access to the neutral coordinate. The object key becomes the
// hierarchical Path; the media type is carried through. S3 has no version or variant
// dimension on the access itself, so Version/Qualifiers/Digest are left to be filled from
// the resource by the caller.
func (c *Coordinator) ToCoordinate(spec runtime.Typed) (*descriptor.Coordinate, error) {
	var s3 v1.S3
	if err := accessspec.Scheme.Convert(spec, &s3); err != nil {
		return nil, fmt.Errorf("error converting access spec to s3: %w", err)
	}
	if s3.ObjectKey == "" {
		return nil, fmt.Errorf("objectKey is required to derive a coordinate")
	}
	return &descriptor.Coordinate{
		Path:      s3.ObjectKey,
		MediaType: s3.MediaType,
	}, nil
}

// FromCoordinate expands any coordinate into an S3 access at the configured target. The
// object key is derived as <keyPrefix>/<path>[/<version>]; bucket, region and endpoint
// come from the target Config, never from the coordinate.
func (c *Coordinator) FromCoordinate(coord *descriptor.Coordinate) (runtime.Typed, error) {
	if coord == nil {
		return nil, fmt.Errorf("coordinate is required")
	}
	if c.target.Bucket == "" {
		return nil, fmt.Errorf("target bucket is required to place a coordinate in s3")
	}
	if coord.Path == "" && coord.Digest == "" {
		return nil, fmt.Errorf("coordinate has neither a path nor a digest to place")
	}

	key := coord.Path
	if key == "" {
		// purely content-addressed source: use the digest as the object key.
		key = coord.Digest
	}
	if coord.Version != "" {
		key = path.Join(key, coord.Version)
	}
	if c.target.KeyPrefix != "" {
		key = path.Join(c.target.KeyPrefix, key)
	}

	return &v1.S3{
		Type:         accessspec.V1VersionedType,
		Region:       c.target.Region,
		BucketName:   c.target.Bucket,
		ObjectKey:    key,
		Endpoint:     c.target.Endpoint,
		UsePathStyle: c.target.UsePathStyle,
		MediaType:    coord.MediaType,
	}, nil
}
