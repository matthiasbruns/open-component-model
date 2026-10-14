# PR #3889 review reproductions (round 2)

PR head: `2e001be77d58898f94c16c68fafb44ee0619b0d2`. Each test asserts the correct behavior, so it fails on the PR head and passes once the finding is fixed.

| ID | Severity | Finding | Run (from `bindings/go`) | Merge base `582b95c` |
| --- | --- | --- | --- | --- |
| F2 | major | Discovery rejects equivalent reference digests of one child (legacy `jsonNormalisation/v3` vs `v4alpha1`, SHA-256 vs SHA-512) as conflicting (`transfer/internal/discovery.go`) | `cd cli && go test ./cmd/ -run '^TestReviewPR3889_F2_'` (CLI transfer) and `cd transfer && go test ./internal/ -run '^TestReviewPR3889_F2_'` | passes |
| F3 | minor | Pinned download rejects valid SHA-512 `genericBlobDigest/v1` resource digests (`cli/cmd/download/shared/verify.go`) | `cd cli && go test ./cmd/download/shared/ -run '^TestReviewPR3889_F3_'` | new code |
| F4 | nit | `ociDigest` rejects digest values spelled `sha256:<hex>` (`cli/cmd/download/shared/verify.go`) | `cd cli && go test ./cmd/download/shared/ -run '^TestReviewPR3889_F4_'` | new code |
