# Review evidence for PR #3979 (head a51251fd2)

Run from the repository root. Needs trivy (0.76), helm, docker, jq.

| ID | Finding | Evidence | Head result |
| --- | --- | --- | --- |
| F1 | OCM-DS-0001 accepts unpinned images pulled by `COPY --from=<image>` and `RUN --mount=...,from=<image>`; `# syntax=` is invisible to rego | `.review/pr-3979/ds-0001-indirect-pulls.sh`; `.github/trivy-checks/ocm-ds-0001_review_test.rego` | script exit 1; `opa test` 17/19 (2 review tests fail) |
| F2 | `impersonate` on every serviceaccount (default chart) passes Trivy (KSV-0043 checks groups only) and Kubescape nsa/mitre (no C-0065; only C-0053, accepted by `controller-needs-api-access`) | `.review/pr-3979/impersonation-unreported.sh` | exit 1 |

Suggested fix for F1: `git apply .review/pr-3979/ocm-ds-0001-fix.diff`. With it the F1 script rejects both
indirect pulls, `opa test` passes 19/19, and the chart scan of the real build files stays at 0 failures.

Exit codes: 0 correct, 1 reproduced, 2 setup failure.
