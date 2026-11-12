<!-- markdownlint-disable MD013 -->
# PR #4206 review reproductions

Repros for findings on [#4206](https://github.com/open-component-model/open-component-model/pull/4206)
(`refactor(bindings)!: migrate to encoding/json/v2 and streaming APIs`).

- PR head: `626e43229b63d66980c175a03d760f868e265ed7`
- Merge base: `c1f262431a40e85cd0424217ebf4830e1274f9b6`
- Branch base: the PR head, so each test can be cherry-picked onto the PR branch as-is.

Every test below asserts the behaviour as it should be, so each one **fails on the PR head and
passes on the merge base**. Run them from `bindings/go`.

## Status

| | finding | state at `ceb9bebf3c` |
| --- | --- | --- |
| F1 | nested `.ocmconfig` specs drop case-variant members | fixed |
| F2 | provider bytes change on a read/write cycle | fixed for `<`, `>`, `&`; see F2b |
| F2b | provider bytes still differ from OCM v1 for U+2028 / U+2029 | **open** |
| F3 | artifact index becomes unopenable | fixed |
| F5 | failed marshal corrupts the plugin response | fixed |
| F6 | wget access and input disagree on URL casing | fixed |
| F4 | no guard that marshal calls pass `JSONOptions` | adopted |

The five Go tests in this branch were written against `626e43229b` and fail there. They all
**pass** on `ceb9bebf3c`, so they now serve as regression tests for the fixes. Each fix was
additionally mutation-tested: reverting it makes the author's own new test fail.

## F1 — nested `.ocmconfig` specs silently drop case-variant members

`bindings/go/runtime/registry.go:461`

`.ocmconfig` is read through `sigs.k8s.io/yaml`, i.e. `encoding/json` v1 semantics, so a member
spelled with non-canonical casing survives into `runtime.Raw.Data` verbatim (`Canonicalize`
reorders members but never changes case). `Scheme.Convert` then decodes that `Raw` with a bare
`encoding/json/v2` `Unmarshal`, which matches names case-sensitively; because
`RejectUnknownMembers` is not set, the member is discarded and `Convert` still returns `nil`.

The credential source contributes nothing and the failure surfaces much later as an anonymous
pull or a 401. Constructor files are unaffected — their JSON-schema gate already rejected case
variants before this PR. `.ocmconfig` has no such gate.

```sh
go test ./oci/spec/credentials/v1/ -run TestReviewPR4206_F1 -count=1
```

| | outcome |
| --- | --- |
| merge base | `dockerConfigFile` and `DockerConfigFile` both load |
| PR head | `DockerConfigFile` decodes to `""`, `Convert` returns `nil` |

Fix direction: decode the nested spec with the leniency of the document it came from, or set
`RejectUnknownMembers` so a dropped member is a hard error. Silently discarding it is the only
outcome a user cannot diagnose.

## F2 — re-encoding an unmodified descriptor changes its provider bytes

`bindings/go/descriptor/runtime/convert_v2.go:341`

The provider is stored as a JSON document embedded in a JSON string. Reading parses it
(`ConvertFromV2Provider`), writing regenerates it (`ConvertToV2Provider`) with its own
`json.Marshal`. That call is the only place where `encoding/json/v2` output reaches the bytes of
a stored descriptor — the descriptor struct itself is marshaled by `sigs.k8s.io/yaml`, i.e.
`encoding/json` v1 (`oci/spec/descriptor/encode.go:22,31`).

v1 escaped `<`, `>` and `&`; v2 does not. A descriptor written by any earlier release whose
provider carries those characters is re-encoded to different bytes — and a different blob
digest — by a read/write cycle that changes nothing.

Measured on a full descriptor through `SingleFileEncodeDescriptor`, the stored YAML goes from
1758 to 1743 bytes, and the provider line is the only difference.

Signature verification is unaffected: normalisation re-parses the provider, so the escaping
cancels out. All four `jsonNormalisation` algorithms are byte-identical between base and head.
This concerns the stored bytes, which the PR describes as unchanged.

```sh
go test ./descriptor/runtime/ -run TestReviewPR4206_F2 -count=1
```

## F2b — the provider string still differs from what OCM v1 wrote (U+2028 / U+2029)

`bindings/go/descriptor/runtime/convert_v2.go:347`

The F2 fix passes `jsontext.EscapeForHTML(true)`, which covers `<`, `>` and `&`.
`encoding/json` v1 also escaped U+2028 and U+2029, and `jsontext` puts those behind a second
option, `EscapeForJS`. So the provider string is still not byte-identical to v1 output when a
label carries one of those two characters.

Verified against the real legacy CLI, not against the Go library in isolation: the
repo-pinned OCM v1 (`conformance/scenarios/compatibility/.env`, `v0.54.0`) builds a component
version, and the v2 CLI from this checkout transfers it.

```text
OCM v1 v0.54.0  {"name":"acme","labels":[{"name":"sep","value":"line1\u2028line2"},{"name":"html","value":"a\u003cb\u003ec\u0026d"}]}
ceb9bebf3c      {"name":"acme","labels":[{"name":"sep","value":"line1<U+2028>line2"},{"name":"html","value":"a\u003cb\u003ec\u0026d"}]}
```

(`<U+2028>` is the raw character; v1 wrote the six bytes `\u2028`.)

```sh
.review/pr-4206/F2b-provider-escaping-vs-ocmv1.sh          # exit 1 on ceb9bebf3c
```

Adding `jsontext.EscapeForJS(true)` next to the existing option makes the output
byte-identical to v1 and the script exit 0. Verified by patching, rebuilding the CLI and
re-running the same transfer; `./descriptor/...` stays green with it, including every
`normalisation` suite, so signatures are unaffected.

Beyond digest churn, this is the case the option exists for: a raw U+2028 is valid JSON but
not valid inside a pre-ES2019 JavaScript string literal (RFC 8259, section 12), which is why
v1 escaped it.

## F3 — an artifact index earlier releases could read no longer opens

`bindings/go/ctf/index/v1/index.go:76`

`DecodeIndex` was `json.NewDecoder(...).DisallowUnknownFields().Decode(...)` on
`encoding/json` v1, which matched member names case-insensitively, so `"Repository"` matched the
known field. Under v2 that member is case-sensitively *unknown*, and
`RejectUnknownMembers(true)` makes it fatal for the whole `artifact-index.json` — the CTF cannot
be opened at all. `UnmarshalRead` additionally consumes the reader to EOF and rejects duplicate
members, both of which `Decoder.Decode` tolerated.

Legacy OCM writes canonical casing, so the trigger is a hand-edited or third-party-generated
index. The failure mode — a readable archive becoming unopenable — is what makes it worth
pinning.

```sh
go test ./ctf/index/v1/ -run TestReviewPR4206_F3 -count=1
```

## F5 — a failed marshal corrupts the plugin response and commits HTTP 200

`bindings/go/plugin/manager/registries/componentversionrepository/handlers.go:50` and the
sibling registries (`:80, 149, 196, 215, 257, 299`, plus `blobtransformer`, `componentlister`,
`credentialplugin`, `credentialrepository`, `digestprocessor`, `input`, `resource`,
`signinghandler`).

`encoding/json` v1 marshaled into an internal buffer, so a failure wrote nothing and the error
branch produced a clean status with an error-only body. `json.MarshalWrite` streams: by the time
the error is returned the first bytes are already on the wire, and the first write has
implicitly committed HTTP 200. Every handler kept its v1-era error branch after the write, so
the error text is appended to a truncated object.

Observed on the head, together with a `superfluous response.WriteHeader` log:

```text
status=200
body="{\"meta\":{\"schemaVersion\":\"v2\"},\"component\":{\"name\":\"ocm.software/review\",\"version\":\"1.0.0\"json: cannot marshal from Go jsontext.Value: unexpected EOF within \"/component/labels/0/value\" after offset 126\n"
```

The client decodes that with `json.UnmarshalRead` (`plugins/call.go:140`) and reports a JSON
syntax error, so the plugin's real failure is lost.

```sh
go test ./plugin/manager/registries/componentversionrepository/ -run TestReviewPR4206_F5 -count=1
```

Reachability note: the trigger used here is an empty but non-nil `jsontext.Value`, which has to
be constructed programmatically — decoding from the wire yields `nil` or `null`, both of which
marshal cleanly. So this is a robustness and diagnosability regression rather than something
reachable from untrusted input.

Fix direction: marshal into a buffer and issue a single `Write` wherever an error branch writes
to the same writer, or stop writing a second payload once the stream has failed.

## F6 — wget access and input disagree on the URL member casing

`bindings/go/wget/spec/input/v1/wget.go:35` versus
`bindings/go/wget/spec/access/v1/wget.go:24`

The PR adds `case:ignore` to the URL member of the access type so legacy specs keep decoding,
and covers it in `wget/spec/access/v1/wget_test.go`. The input type declares the same member as
plain `json:"url"` and did not get the tag, so within one resource `URL:` is accepted under
`access:` and silently dropped under `input:`. `Validate()` then reports `url is required` for a
document that does supply it.

Both types are decoded through `runtime.Scheme` here, the path production uses for a nested
spec, so the two differ only in the type being decoded.

```sh
go test ./wget/spec/input/v1/ -run TestReviewPR4206_F6 -count=1
```

## F4 — proposed guards (these pass on the head)

Not defects. They pin invariants the PR relies on but nothing currently enforces.

```sh
go test ./runtime/ ./descriptor/runtime/labels/artifactref/v1alpha1/ -run TestReviewPR4206_F4 -count=1
```

- **F4a** walks the module and asserts every `encoding/json/v2`
  `Marshal`/`MarshalWrite`/`MarshalEncode` call passes `runtime.JSONOptions`. The forbidigo rule
  added by this PR bans the v1 entrypoints but cannot inspect arguments, so a plain
  `json.Marshal(v)` lints clean, compiles, and silently reintroduces random map ordering and
  `{}` / `[]` for a nil map or slice. There is no golden-bytes canary either, because the stored
  descriptor is v1-produced (see F2), so such a regression would keep the suite green. The one
  documented exception is `runtime/unstructured.go:80`, a `MarshalJSONTo` that deliberately
  inherits its caller's options from the encoder it was handed.
- **F4b** covers `runtime.JSONOptions` directly; package `runtime` has no `json_test.go`.
  Dropping `Deterministic` fails ~30 tests and dropping `FormatNilSliceAsNull` ~26, but dropping
  `FormatNilMapAsNull` fails exactly one — and that test is about a pointer marshaler encoding a
  zero time as `null`, with the nil-map shape caught only incidentally by its JSON comparison
  helper.
- **F4c** covers the nil-map shape on content that can be signed. `artifactref.Reference.Identity`
  is a `runtime.Identity` tagged `json:"identity"` with no omit option, so the member is always
  emitted, and these bytes land inside the value of the `ocm.software/artifact-references` label,
  which an element can mark `signing: true`. `artifactref_test.go` has no case with an unset
  `Identity`.

## Verified sound, for the record

No defect found in any of these, each checked against the merge base:

- `Deterministic(true)` sorts string map keys byte-wise, identical to v1, so map ordering is
  digest-stable.
- `jsontext.Value.Canonicalize()` matches the removed `github.com/cyberphone/json-canonicalization`
  over 20 035 inputs; the only divergences are the old library being *more* restrictive
  (rejecting top-level scalars) and `1e400`, which it rejected and the new one clamps.
- All four `jsonNormalisation` algorithms and the `sigs.k8s.io/yaml` round trip are
  byte-identical between base and head on an adversarial descriptor (integers beyond 2^53,
  HTML-special characters, emoji, `0` / `false` / `null` / `{}` / `[]` label values, non-ASCII
  map keys). Signing is safe.
- `encoding/json.Number` marshals byte-identically under v1 and v2, including beyond 2^53, `1.0`,
  `-0` and the error cases.
- The 126 `omitempty` → `omitzero` rewrites are type-correct; the remaining `omitempty` on
  scalars is confined to `kubernetes/controller/api/v1alpha1`, which the PR deliberately left
  alone and which serializes through apimachinery, i.e. v1 semantics.
- The legacy-casing set is complete: `wget` `URL` is the only uppercase divergence in
  `open-component-model/ocm`, and the PR covers it for the access type (see F6 for the input
  type).
