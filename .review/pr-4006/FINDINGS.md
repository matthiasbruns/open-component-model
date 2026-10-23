# Review evidence for open-component-model/open-component-model#4006

Reviewed head: `51106f98533e047df757e58119d00b9fec2fca43` (merge base `3611c8024b38695631ddee70825ec12842643ed4`).
Every test/script asserts the correct behavior: it fails on the reviewed head and passes once fixed.

| ID | Severity | Category | Location | Claim | Head | Base | How to run |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F1 | major | correctness | bindings/go/signing/digest.go:30 | v3 -> v4alpha1 mapping does not reproduce legacy v3 bytes when labels carry a version | reproduced | reproduced | `cd bindings/go && go test -count=1 -run '^TestReviewPR4006_F1_LegacyV3SignaturesVerify$' ./signing/...` |
| F2 | minor | docs | bindings/go/cli/cmd/sign/component-version/cmd.go:278 | Website and security docs still present v4alpha1 as the current/default algorithm | reproduced | not-reproduced | `bash .review/pr-4006/F2-docs-still-name-v4alpha1-default.sh` |
| F3 | nit | docs | bindings/go/repository/pin.go:144 | OCI-pin fallback error still suggests a v4alpha1 normalized pin | reproduced | not-reproduced | `bash .review/pr-4006/F3-pin-hint-suggests-v4alpha1.sh` |
