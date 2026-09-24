// Package verify provides reusable verification of OCM resource downloads.
// NewResourceRepository selects a verifier before downloading, preferring an
// optional repository-provided ResourceVerifierProvider to a generic fallback.
// The default fallback verifies generic blob digests when present; RequireDigest
// can be configured with WithFallbackResourceVerifierProvider to reject unsigned
// resources. Provider errors never trigger fallback verification.
//
// Verification may be streaming. Consumers must read to EOF and check errors
// before publishing or trusting downloaded content. Byte-level verification is
// implemented by the blob/verification package.
package verify
