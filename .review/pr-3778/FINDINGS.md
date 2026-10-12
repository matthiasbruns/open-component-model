# Review evidence for open-component-model/open-component-model#3778

Reviewed head: `0c97095d2571c8752c55db4d3b0ab896d1391d5f` (merge base `6ea63189d442f1bdf9538d30996167dd01cabbff`).

Two kinds of test live here:

- **Regression tests** (F1, N1) assert the correct behaviour: they fail on the reviewed head and pass once fixed.
- **Characterisation tests** (Q1, Q2, Q3, store probe) assert what the head does today and pass on it. They document behaviour for discussion, not a fix target.

All commands run from `bindings/go/s3`.

| ID | Kind | Location on head | Claim | Head | Base | Run |
| --- | --- | --- | --- | --- | --- | --- |
| F1 | regression | `s3/internal/download/download.go:123` | A part-1 response without `Content-Range` is stored as the whole object although the store reports 3 parts; the descriptor gets sha256(part 1) and later downloads return the same truncated bytes without error | fails | passes | `go test ./internal/download/ ./repository/ -count=1 -run 'TestReviewPR3778_F1'` |
| Q3 | characterisation | `s3/internal/download/span_checksum.go:36` | The per-part checksum check runs only with `x-amz-checksum-type: COMPOSITE`; a corrupt part without that header is written unchecked | passes | n/a (new code) | `go test ./internal/download/ -count=1 -run 'TestReviewPR3778_Q3'` |
| Q1 | characterisation | `s3/repository/resource_repository.go:294` | The HEAD fast path cannot be disabled and ignores `maxDownloadSize` | passes | n/a | `go test ./repository/ -count=1 -run 'TestReviewPR3778_Q1'` |
| Q2 | characterisation | `s3/repository/resource_repository.go:294` | A wrong advertised checksum becomes the digest; the SDK rejects the later download | passes | n/a | `go test ./repository/ -count=1 -run 'TestReviewPR3778_Q2'` |
| N1 | regression (pre-existing) | `s3/repository/resource_repository.go:218` | A `sha-256:` / `SHA-256:` value prefix is rejected although `VerifyDownload` accepts it | fails | fails | `go test ./repository/ -count=1 -run 'TestReviewPR3778_N1'` |
| N2 | script | `s3/repository/resource_repository.go:272` | `requestedAlgorithm` duplicates `repository.hashAlgorithm` | exit 1 | n/a | `bash ../../../.review/pr-3778/N2-duplicate-algorithm-mapping.sh` |

## Store probe (RustFS `1.0.0-rc.6`, MinIO `RELEASE.2025-09-07T16-13-09Z`)

`go test ./integration/ -count=1 -v -run 'Test_Integration_ReviewPR3778_StoreProbe'` (testcontainers, local Docker)

- Both stores send `Content-Range` on part GETs, so neither triggers F1.
- HEAD of a multipart SHA-256 upload: both report `x-amz-checksum-type: COMPOSITE` with a `…-N` suffix, also for a 1-part upload. The fast path correctly falls back to downloading.
- Part GETs: MinIO sends the exact per-part SHA-256 but **no** `x-amz-checksum-type`; RustFS sends no checksum. The per-part check (Q3) therefore never runs on either store.
- PutObject with a wrong `x-amz-checksum-sha256`: RustFS rejects it with `BadDigest`, MinIO with `XAmzContentChecksumMismatch`.
