# Review evidence for open-component-model/open-component-model#3595

Reviewed head: `0ce47b76d7d7cc49756d463315e11894028b9db0` (merge base `63d9bde9492d9b0b43fe8027d07fbe5f036520e9`).
Every test/script asserts the correct behavior: it fails on the reviewed head and passes once fixed.

| ID | Severity | Category | Location | Claim | Head | Base | How to run |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F1 | blocker | docs | docs/adr/0030_vm_image_packaging.md:1 | ADR number 0030 is already taken on main | reproduced | not-reproduced | `bash .review/pr-3595/F1-adr-number-collision.sh` |
| F2 | major | docs | docs/adr/0030_vm_image_packaging.md:265 | Decision 9 duplicates and contradicts the merged ADR 0031 | reproduced | inconclusive | `bash .review/pr-3595/F2-decision9-conflicts-adr0031.sh` |
| F3 | major | correctness | docs/adr/0030_vm_image_packaging.md:180 | Flow C (OCIImage) does not deliver the disk bytes | reproduced | reproduced | `cd bindings/go && go test -count=1 -run '^TestReviewPR3595_F3_' ./oci/...` |
| F8 | major | correctness | docs/adr/0030_vm_image_packaging.md:93 | compress: true with a declared media type drops the +gzip suffix | reproduced | reproduced | `bash .review/pr-3595/F8-compress-drops-gzip-suffix.sh` |
| F4 | major | docs | docs/adr/0030_vm_image_packaging.md:217 | software.ocm.vm/format violates the spec's label naming scheme | reproduced | inconclusive | `bash .review/pr-3595/F4-label-name-not-owned-domain.sh` |
| F7 | minor | docs | docs/adr/0030_vm_image_packaging.md:107 | Flow A example is rejected by the CLI (24.04.1 is not semver) | reproduced | inconclusive | `bash .review/pr-3595/F7-flow-a-example-not-runnable.sh` |
| F5 | minor | docs | docs/adr/0030_vm_image_packaging.md:237 | Decision 8 misdescribes ocm download resource defaults | reproduced | reproduced | `bash .review/pr-3595/F5-download-default-policy-and-name.sh` |
| F9 | minor | docs | docs/adr/0030_vm_image_packaging.md:164 | Wget digest check happens while reading, not before returning the blob | reproduced | reproduced | `cd bindings/go && go test -count=1 -run '^TestReviewPR3595_F9_' ./wget/repository/...` |
| F6 | minor | docs | .github/config/wordlist.txt:65 | wordlist.txt was removed on main | reproduced | not-reproduced | `bash .review/pr-3595/F6-wordlist-removed-on-main.sh` |
