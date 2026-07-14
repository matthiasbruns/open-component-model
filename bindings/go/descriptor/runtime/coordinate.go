package runtime

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Coordinate is a technology-independent locator for a resource's content. It is the
// shared coordinate system that lets a resource move between storage technologies
// without any uploader having to understand another technology's access spec.
//
// A source access is reduced to a Coordinate (produce side), and a Coordinate is
// expanded into a target access (consume side). Every field except MediaType is
// optional; a given backend fills only what it has, so the stress cases degrade
// gracefully (version-less URLs leave Version empty, content-addressed stores fill
// only Digest, and so on).
type Coordinate struct {
	// Path is a "/"-separated hierarchical name in the source namespace,
	// e.g. an OCI repository "acme/podinfo", a maven "com/acme/lib", an S3 key
	// "artifacts/data". Empty for purely content-addressed sources.
	Path string `json:"path,omitempty"`

	// Version is the mutable version/tag (OCI tag, semver, npm version).
	// Empty for version-less sources (a raw URL, a filesystem blob).
	Version string `json:"version,omitempty"`

	// Qualifiers are variant dimensions distinguishing artifacts that share the same
	// Path+Version (maven classifier/packaging, os/arch, python abi/platform).
	// They mirror a resource's extraIdentity.
	Qualifiers map[string]string `json:"qualifiers,omitempty"`

	// Digest is an immutable content pin "<algo>:<hex>". It is the primary locator
	// for content-addressed stores and an optional pin elsewhere.
	Digest string `json:"digest,omitempty"`

	// MediaType tells a target how to store/interpret the content
	// (an OCI manifest vs a raw object vs a tar).
	MediaType string `json:"mediaType,omitempty"`
}

// ReferenceHint travels with a localized resource. It carries the neutral Coordinate
// for cross-technology placement and, optionally, the exact typed Origin access for
// lossless same-technology restore or provenance.
type ReferenceHint struct {
	Coordinate Coordinate `json:"coordinate"`
	// Origin is the original access spec, kept verbatim. Optional: used for exact
	// same-technology restore and provenance; cross-technology placement uses only
	// the Coordinate.
	Origin *runtime.Raw `json:"origin,omitempty"`
}
