# Scope vs issue

No linked issues or acceptance criteria appear in PR #3940 or its prerequisite PRs #3938/#3939. No parent epic is identified. Requirements therefore remain undocumented independently of these PRs; review uses the repository blob contracts, established sibling implementations, and HTTP standards, and treats the PR description and ADR as claims to validate.

Full diff scope: mTLS transport preservation, streaming wget downloads, transparent negotiated gzip for wget input, identity probing, sizes and checksum handling, callers, tests and documentation. Compression-specific scope is separate from inherited mTLS/streaming changes. Controller reconciliation and deletion are outside the changed area.

Intended outcomes to verify: stored content independent of negotiated coding; stored archives retained; correct known/unknown sizes; content and transfer checksums verified at consumption; no repeated unsafe methods; cancellation and cleanup observable; access returns original bytes. No issue-level met/partial/missing assessment can be made without independent acceptance criteria.
