# HTTP uploader review scenarios

Run from this directory with Bash. Requires Go (the version in `bindings/go/go.mod`),
its module dependencies, `jq`, `curl`, and standard Unix utilities. No Python,
Docker, external registry, or real credentials are needed. The Go fixture listens
only on loopback; allow localhost traffic through any proxy configuration.

```sh
bash default-method.sh
bash explicit-put.sh
bash extra-identity.sh
bash nested-literal.sh
bash macro-shadow.sh
bash triple-quotes.sh
bash redirect.sh
bash query-token.sh
bash oci-digest.sh
bash early-response.sh
```

Each script sources `common.sh`, starts an isolated fixture, constructs a source
component, and invokes the actual CLI through the helper (`go run main.go`).
The exit trap stops the fixture, but retains the printed artifact directory:
constructor and configuration JSON, per-command `.stdout`/`.stderr` logs,
`requests.jsonl` (header values are arrays), object files, and state snapshots.
Default/explicit-method scenarios also download from the actual target component
and compare the result and stored object. CLI calls default to a 90-second timeout;
set `OCM_TIMEOUT` to override it. Cold Go builds may need extra time.

- **CONTROL PASSED**: default-method upload/readback works as expected.
- **REPRODUCED / NOT REPRODUCED**: diagnostic bug outcome, not an inverted test
  assertion requiring the bug to remain. Successful fixes should still exit zero.
- **OBSERVATION**: redirect and early-response policy behavior, without demanding
  acceptance or rejection. Early-response uses a 32 MiB source.
- Unexpected setup/control or unrelated CLI failures exit nonzero. The deliberate
  exception is OCI: unrecognized transfer failures are printed as observations
  under NOT REPRODUCED, not rejected by a harness guard.

The template cases inspect `X-Review-Template`; extra-identity selects tier
`public` and resolves `resource.name` in the destination URL. OCI compares uploaded
bytes with the fixture manifest digest. Query-token uses only a clearly synthetic
value and checks its exposure in a malformed-HTTP transport error. Retained logs
intentionally include that synthetic token; never substitute a real secret.
