# Review evidence for open-component-model/open-component-model#3978

Reviewed head: `9bcb427b097e9141ec655f8675156e2ff9a37013` (merge base `c639f6842030e7b081e3fbf3ca144fbcadeea847`).
Every test/script asserts the correct behavior: it fails on the reviewed head and passes once fixed.

| ID | Severity | Category | Location | Claim | Head | Base | How to run |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F1 | major | correctness | bindings/go/cli/Containerfile:23 | CLI images: /tmp and /.cache are writable only by UID 65532 | reproduced | not-reproduced | `bash .review/pr-3978/F1-cli-image-other-uid-no-tmp.sh` |
| F2 | minor | gap | .github/workflows/image-scan.yml:186 | Provenance step checks the FIPS build settings of the entrypoint only, not cosign | reproduced | inconclusive | `bash .review/pr-3978/F2-provenance-skips-cosign.sh` |
| F3 | minor | correctness | .github/stig/tailoring.xml:13 | ASLR/NX rules pass on the runner kernel, not on the image | reproduced | inconclusive | `bash .review/pr-3978/F3-sce-rules-check-runner-kernel.sh` |
