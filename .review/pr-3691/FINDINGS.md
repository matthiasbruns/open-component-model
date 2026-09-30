# Review evidence for open-component-model/open-component-model#3691

Reviewed head: `1ceaa2fa7e40fce482519424bda0bd2e1c7ff687` (merge base `a6806eeac786e2cec3c1813a1ee39df77229d2b1`).
Every test/script asserts the correct behavior: it fails on the reviewed head and passes once fixed.

| ID | Severity | Category | Location | Claim | Head | Base | How to run |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F1 | major | correctness | bindings/go/gpg/signing/handler/internal/gpgbinary/gpgbinary.go:391 | Revoked/expired pinned key is accepted when another trusted key co-signs | reproduced | inconclusive | `cd bindings/go && go test -count=1 -run '^Test_Integration_ReviewPR3691_F1_RevokedPinnedKeyWithCosigner$' ./gpg/signing/handler/` |
| F2 | major | gap | bindings/go/cli/Containerfile:9 | Published CLI image (FROM scratch) loses GPG sign/verify; breaking note does not mention it | reproduced | not-reproduced | `bash .review/pr-3691/F2-container-image-lacks-gpg.sh` |
| F3 | minor | correctness | bindings/go/gpg/signing/handler/internal/gpgbinary/gpgbinary.go:282 | Isolated home under $TMPDIR breaks signing when TMPDIR is long (gpg-agent socket path limit) | reproduced | inconclusive | `cd bindings/go && go test -count=1 -run '^Test_Integration_ReviewPR3691_F3_LongTMPDIR$' ./gpg/signing/handler/` |
