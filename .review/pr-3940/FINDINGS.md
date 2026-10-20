# PR 3940 regression evidence

Review target: `69ec429972018edf67ce1a69b072cd90e23412d6`.
Merge base: `eaa272a5d97c189285a92aca2f882ce5a05aa21f`.
Both findings are introduced by the compression change in this PR. The same
test file compiles and passes on the merge base. Tests use local HTTP servers;
no Docker, credentials, external endpoints, or added dependencies are needed.

The tests assert correct behavior, so their failures on the PR head are expected.

## F1: known-size copies skip Content-Digest verification

Location: [blob.go, line 249](https://github.com/open-component-model/open-component-model/blob/69ec429972018edf67ce1a69b072cd90e23412d6/bindings/go/wget/internal/download/blob.go#L249).

The input receives negotiated gzip with an incorrect SHA-256 Content-Digest.
The identity probe supplies the correct decoded size. The gzip writer flushes
before finishing, allowing the last payload byte to arrive before gzip EOF.
Reading through EOF rejects the incorrect digest, but `blob.Copy` reads only
the advertised size and returns nil, including after closing the reader. This
bypasses Require checksum verification for a supported repository consumer.

Expected: both consumption paths reject the incorrect digest.
Actual: the EOF control passes; the blob.Copy assertion fails because its error
is nil. Ensure gzip completion and verification also run when a consumer stops
at the advertised size.

```sh
cd bindings/go
go test ./wget/input -run '^TestReviewPR3940_F1_' -count=1 -v
```

Head: FAIL (`An error is expected but got nil`). Merge base: PASS.

## F2: a failed range probe silently unpacks a stored archive

Location: [representation.go, lines 33-48](https://github.com/open-component-model/open-component-model/blob/69ec429972018edf67ce1a69b072cd90e23412d6/bindings/go/wget/internal/download/representation.go#L33).

The server returns a stored gzip file with Content-Encoding: gzip for all full
GETs, including an explicit Accept-Encoding: identity request. It rejects Range
requests with 403. The test establishes the identity response bytes independently
before invoking the input method.

Expected: preserve the 52-byte stored archive. Actual: the input succeeds and
returns its unpacked 22-byte payload. Failure to classify compression is not
evidence that it was negotiated. Fetching the full identity representation is a
safe fallback; returning an explicit classification error is also preferable to
silently changing the artifact (the preservation regression test currently
requires the successful fallback).

```sh
cd bindings/go
go test ./wget/input -run '^TestReviewPR3940_F2_' -count=1 -v
```

Head: FAIL (52-byte archive differs from 22-byte payload). Merge base: PASS.

## Commands and captured results

Run both repros from the evidence branch:

```sh
cd bindings/go
go test ./wget/input -run '^TestReviewPR3940_' -count=1 -v
```

- [Head test output](head-tests.txt): exit 1; both findings reproduced.
- [Merge-base output](base-tests.txt): exit 0; both tests pass after adding the
  identical test file to an archive of the merge base.
- [Existing compression tests](existing-tests.txt): exit 0 for
  `go test ./wget/input ./wget/internal/download -run 'TestDownload_CompressTransfer|TestProcessResource_ContentEncoding' -count=1 -v`.
- Repository task `task bindings/go:test -- -run TestReviewPR3940 -count=1`
  also ran and failed on these two regression assertions.
- `task tools:lint` was attempted but failed to typecheck: the pinned
  golangci-lint v2.13.2 cannot decode the installed Go 1.27.2 export data
  (`export data version 5 is greater than maximum supported version 4`).
  This is a local tooling limitation, not a passing lint result.

No production changes are included. Local integration/race testing, the full
unit suite, and documentation generation/cross-reference checks were not run.
