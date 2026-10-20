# PR 3939: encoded HTTP checksum regression

Verified head: `3d457eb3cc51e7b7020dec35b43bb4d9361d9887`.
Actual merge base: `eaa272a5d97c189285a92aca2f882ce5a05aa21f`.
GitHub base branch head: `d090da3ad87ba2f95f687cfb906e454fb465f2ec`.

## Scope

No requirements issue is linked. This evidence checks preservation of HTTP checksum
semantics when using the existing public `repository.WithHTTPClient` option.
The default OCM client is unaffected by this specific regression.

## Confirmed finding (P2)

The removal of the `Accept-Encoding: identity` safeguard in
`bindings/go/wget/internal/download/download.go`, adjacent to lines 152–157,
allows Go's standard transport to negotiate gzip and transparently decode the
response before the checksum verifier hashes it.

The public-API test serves correctly checksummed content, negotiating gzip only
when the request accepts it. It uses checksum mode `Require`.

| Variant | Current head | Merge-base Wget overlay | Restored safeguard overlay |
| --- | --- | --- | --- |
| Default OCM client | Pass | Pass | Pass |
| Injected standard client | Fail | Pass | Pass |
| Standard client with explicit identity header | Pass | Pass | Pass |
| Low-level encoded-byte regression | Fail | Pass | Pass |

The failing public-API case reports:

```text
Accept-Encoding="gzip" bytes=16
SHA-256 checksum mismatch:
expected 1089bc7e484619147be5e93bc908fdca2dd5f98faed04beef2664fa615af9334
computed 42bd420cc2f99e68e60005fa7c28fc2f60e4e04ee160d9dd3b98e72fc2954f98
```

Both `Read` and `Close` return the mismatch. This rejects a valid download;
it is not evidence of accepting corrupted content.

## Reproduce

Run from the checkout root:

```sh
python3 .review/pr-3939/check_encoding.py head
python3 .review/pr-3939/check_encoding.py base
python3 .review/pr-3939/check_encoding.py restored
```

Observed exit codes: head **1**, base **0**, restored **0**.
Full outputs: `encoding-head.log`, `encoding-base.log`, `encoding-restored.log`.

The script never modifies PR source. Base mode overlays tracked Wget Go files
from the merge base, retaining the current dependency graph and HTTP package.
The public regression test receives only the baseline constructor's required
leading `nil` filesystem-config argument. This is package-level regression
evidence, not a full merge-base checkout test.

Restored mode overlays only these lines before redirect handling:

```go
if len(o.DigestAlgorithms) > 0 {
    httpReq.Header.Set("Accept-Encoding", "identity")
}
```

## Draft review comment

**[P2] Preserve encoded bytes when verifying with an injected HTTP client**

Removing the identity-encoding safeguard makes a standard client supplied through
`repository.WithHTTPClient` transparently decompress gzip before checksum
verification. A server returning a valid checksum for its response then fails
with `SHA-256 checksum mismatch` under `Require`. The default OCM client is
unaffected because it disables transparent decompression.

`TestReviewPR3939_ValidHTTPChecksumWithStandardClient` reproduces this through
`DownloadResource`: the standard-client case fails, while the default-client and
explicit-identity controls pass. The same test passes against the merge-base Wget
code and with only the removed identity safeguard restored via a Go overlay.
Please retain that safeguard or otherwise ensure HTTP checksum verification sees
the encoded bytes with injected clients.

## Validation limits

GitHub head and full diff were unchanged when this evidence was prepared; CI was
green and all three existing review threads were resolved. The earlier three
regression tests passed in the preceding recheck.

The required local lint gate remains blocked by the previously reproduced
Go 1.27.2 export-data incompatibility in the pinned linter. It was not rerun for
this evidence-only follow-up. No full local suite, Docker tests, or CLI scenario
was run in this follow-up. This evidence does not constitute a completed approval
review. Evidence is published separately to Matthias's review fork; the author's
branch is unchanged. No GitHub comments were posted.
