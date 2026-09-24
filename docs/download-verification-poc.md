# Resource download verification PoC

This is an incremental design experiment on top of
[PR #3641](https://github.com/open-component-model/open-component-model/pull/3641),
pinned at `a423aea3bb205c4abe4bfc2f534a6aba5640753e`. It is not a claim that every
OCM resource access path is now verified.

## Design

Backend authors implement `repository.ResourceBackend`, whose transport method is
`FetchResource`. Consumers use `repository.ResourceRepository.DownloadResource`.
The different method sets prevent accidentally using a raw backend as the public
repository interface. A shared facade validates the expected digest before
fetching, then attaches streaming verification. GitHub, S3, and wget constructors
install that facade automatically, and keep their raw backends private.

```text
consumer: DownloadResource
    -> shared facade: parse expected digest and apply policy
        -> backend: FetchResource
        -> blob/verification.Wrap
    -> consumer reads to EOF and checks errors
```

This enforces composition, not honesty: Go interfaces cannot prove arbitrary
implementations perform verification. A separately written implementation of
`ResourceRepository` can still bypass the facade. The guarantee applies to the
constructors migrated in this PoC.

## Packaging

- `blob/verification`: byte-level verification against an independent digest;
  no descriptor imports, access-type knowledge, or missing-digest policy.
- `repository/resource_download.go`: resource semantics and the shared facade.
- `repository/verify_digest.go`: descriptor digest interpretation. Generic blob
  normalization is supported; empty normalization retains legacy compatibility.
  Other normalization algorithms fail before fetching.

The facade stays in `repository` for this experiment, which already owns generic
repository behavior. It could move to `repository/resource` if that layer grows.
No new dependency or generator output is needed.

## Policies and guarantees

`NewVerifiedResourceRepository` defaults to `VerifyIfPresent`, preserving the
original PR's unsigned-resource behavior. Missing digests and explicit exclusions
pass through with a warning, not a verified label.

`NewResourceRepositoryWithVerification(backend, RequireDigest)` rejects both
missing digests and explicit exclusions before fetching. The migrated backend
constructors currently select the compatibility policy; exposing strict policy
through their options is a follow-up design decision.

The expected digest is captured before transport executes. A backend modifying
the resource cannot replace the independent expectation being checked.

Verification is lazy. Successful `DownloadResource` means the verification reader
is installed, not that the bytes have passed verification. Consumers must read to
EOF and check errors. Partial reads fail on close; ignoring both completion and
close errors is not safe. Digest metadata still describes the underlying blob,
not proof that it matches the descriptor. Authenticating the descriptor remains
a separate signing concern.

`blob.Copy` now probes EOF after the advertised size, so correctly sized blobs
complete verification and trailing bytes cannot escape the check. Extra bytes
are not written to the destination. Output written before a mismatch must still
be discarded by the consumer.

## Scope and follow-ups

- OCI, Helm, local-resource reads, sources, and external-plugin enforcement are
  not migrated. OCI needs a representation-aware verifier rather than hashing
  arbitrary archive bytes against an OCI resource digest.
- The resource registry continues to dispatch to repositories. Migrated built-in
  repositories are protected by their constructors, but wrapping the entire
  registry with a generic-blob verifier would incorrectly include OCI.
- GitHub digest establishment retains the PR's metadata path through the download
  facade. A dedicated raw-fetch capability for the digest processor would make
  that separation explicit without exposing raw fetching to ordinary consumers.
- `VerifyDownload` remains available for compatibility and focused tests, but
  migrated backend downloads no longer invoke it themselves.
- A future eager API could return an owned, immutable verified-content handle
  after staging and checking the whole download. That would prevent processing
  bytes before verification, at the cost of storage and first-byte latency.
- Digest parser consolidation and a normalization-aware verifier registry remain
  separate work; no fallback treats unsupported normalization as raw bytes.

## Review and validation

The fork PR is based on a snapshot branch of PR #3641 so its diff shows only this
experiment, not the original verification implementation. No changes are pushed
to the original author's branch.

Contract tests cover matching and mismatching downloads, strict and permissive
policy, pre-fetch rejection, backend errors and forwarding, and mutation of the
expected digest. Blob tests cover exact-size success, empty content, short reads,
trailing bytes, and read errors after the advertised content.

Run the focused packages from `bindings/go`:

```sh
go test ./blob/... ./repository/... ./github/... ./s3/repository/... ./wget/repository/...
```

Repository-wide checks:

```sh
task tools:lint
task bindings/go:test
```
