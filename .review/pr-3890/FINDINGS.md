# Review evidence for open-component-model/open-component-model#3890

Reviewed head: `599c0a156874dcf2ed8cc75b39265c458e959195` (merge base `69415daa439a916b45db2c4be925ae517a4ae4cb`).
Every test/script asserts the correct behavior: it fails on the reviewed head and passes once fixed.

| ID | Severity | Category | Location | Claim | Head | Base | How to run |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F3 | minor | docs | website/content/docs/reference/input-and-access-types.md:455 | Callouts announce removal of GitHub/v1, which published component versions still need | reproduced | not-reproduced | `bash .review/pr-3890/F3-removal-claim-contradicts-support.sh` |
