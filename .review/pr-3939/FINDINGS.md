# Review evidence for open-component-model/open-component-model#3939

Reviewed head: `2326d45f3531ca1e9e7b7c0b6a19eec33485bc3f` (merge base `c639f6842030e7b081e3fbf3ca144fbcadeea847`).
Every test/script asserts the correct behavior: it fails on the reviewed head and passes once fixed.

| ID | Severity | Category | Location | Claim | Head | Base | How to run |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F2 | minor | correctness | website/content/docs/guides/pack/add-http-resources.md:460 | A per-host timeout on the source host now also limits the upload to the target | reproduced | inconclusive | `cd bindings/go && go test -count=1 -run '^TestReviewPR3939_F2_StreamingTransferWithSourceHostTimeout$' ./wget/transformation/` |
| F1 | minor | correctness | bindings/go/wget/internal/download/blob.go:337 | A reader closed before the end of the body skips the checksum policy without an error | reproduced | not-reproduced | `cd bindings/go && go test -count=1 -run '^TestReviewPR3939_F1_PartialReadMustNotBypassChecksumPolicy$' ./wget/input/` |
