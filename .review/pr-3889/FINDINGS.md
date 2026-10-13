# PR #3889 review reproductions

PR head: `bee201505f71b369c3bf8e32b8fd8d731a104266`. Each test asserts the correct behavior, so it fails on the PR head and passes once the finding is fixed.

| ID | Severity | Finding | Run (from `bindings/go/<module>`) |
| --- | --- | --- | --- |
| F1 | major | A root pin is silently replaced by another root's reference digest (`transfer/internal/discovery.go`) | `cd transfer && go test ./internal/ -run '^TestReviewPR3889_F1_' -count=20` |
| F2 | minor | Pinned download rejects valid SHA-512 resource digests (`cli/cmd/download/shared/verify.go`) | `cd cli && go test ./cmd/download/shared/ -run '^TestReviewPR3889_F2_'` |
| F3 | nit | `ociDigest` rejects digest values spelled `sha256:<hex>` (`cli/cmd/download/shared/verify.go`) | `cd cli && go test ./cmd/download/shared/ -run '^TestReviewPR3889_F3_'` |

Results on the PR head: F1 fails 20/20 (also with `-race`, no data race reported), F2 and F3 fail.
