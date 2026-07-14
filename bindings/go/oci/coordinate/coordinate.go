// Package coordinate implements the technology-independent coordinate mapping for the
// OCI access type.
package coordinate

import (
	"fmt"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Config describes where FromCoordinate places content in an OCI environment.
type Config struct {
	// RegistryBaseURL is the base to place artifacts under, e.g. "ghcr.io/acme".
	RegistryBaseURL string
}

// Coordinator maps between OCI accesses and neutral coordinates.
type Coordinator struct {
	target Config
}

var _ repository.Coordinator = (*Coordinator)(nil)

// New returns a Coordinator that places content under the given registry base.
func New(target Config) *Coordinator {
	return &Coordinator{target: target}
}

// ToCoordinate reduces an OCI access to the neutral coordinate: the host-stripped
// repository becomes Path, the tag becomes Version, a digest (if any) becomes Digest.
func (c *Coordinator) ToCoordinate(spec runtime.Typed) (*descriptor.Coordinate, error) {
	var img v1.OCIImage
	if err := ociaccess.Scheme.Convert(spec, &img); err != nil {
		return nil, fmt.Errorf("error converting access spec to oci image: %w", err)
	}
	ref, err := looseref.ParseReference(img.ImageReference)
	if err != nil {
		return nil, fmt.Errorf("invalid oci image reference %q: %w", img.ImageReference, err)
	}
	if ref.Repository == "" {
		return nil, fmt.Errorf("oci image reference %q has no repository", img.ImageReference)
	}
	coord := &descriptor.Coordinate{
		Path:      ref.Repository, // registry host intentionally dropped (environment-specific)
		Version:   ref.Tag,
		MediaType: "application/vnd.oci.image.manifest.v1+tar+gzip",
	}
	if d := ref.Reference.Reference; strings.HasPrefix(d, "sha256:") {
		coord.Digest = d
	}
	return coord, nil
}

// FromCoordinate expands any coordinate into an OCI access under the configured registry
// base: <base>/<path>:<version> (or @<digest>, or :latest when neither is set).
func (c *Coordinator) FromCoordinate(coord *descriptor.Coordinate) (runtime.Typed, error) {
	if coord == nil {
		return nil, fmt.Errorf("coordinate is required")
	}
	if c.target.RegistryBaseURL == "" {
		return nil, fmt.Errorf("target registry base URL is required to place a coordinate in oci")
	}
	if coord.Path == "" {
		return nil, fmt.Errorf("coordinate has no path to place as an oci repository")
	}

	ref := fmt.Sprintf("%s/%s", strings.TrimRight(c.target.RegistryBaseURL, "/"), coord.Path)
	switch {
	case coord.Version != "":
		ref += ":" + coord.Version
	case coord.Digest != "":
		ref += "@" + coord.Digest
	default:
		ref += ":latest"
	}

	return &v1.OCIImage{
		Type:           runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion),
		ImageReference: ref,
	}, nil
}
