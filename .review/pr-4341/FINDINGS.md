# Review evidence for open-component-model/open-component-model#4341

Reviewed head: `d84721ee8fadbed42069107ae3ae6be5c5553c26` (merge base `424c3a57c9d1b98a2849e34cf96a4a8d030d3620`).
Every test/script asserts the correct behavior: it fails on the reviewed head and passes once fixed.

| ID | Severity | Category | Location | Claim | Head | Base | How to run |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F1 | major | correctness | bindings/go/credentials/synced_dag.go:163 | Nested prefixes resolve to the broader entry: the lexical tie-break prefers the less specific pattern | reproduced | reproduced | `cd bindings/go && go test -count=1 -run '^TestReviewPR4341_F1_' ./credentials/` |
| F2 | minor | docs | website/content/docs/changelog/migrate-legacy-credentials.md:83 | Migration guide still says the new OCM does not rank matching entries | reproduced | not-reproduced | `bash .review/pr-4341/F2-stale-migration-guide.sh` |
