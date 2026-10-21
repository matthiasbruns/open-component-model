# PR 3887: list-valued signing label ordering

The regression test asserts byte equality with gardener/cc-utils, not JSON semantic equality.
cc-utils retains `[{"a2":1,"a10":2}]`; the PR emits `[{"a10":2,"a2":1}]`.

## Reproduce

From this branch checkout:

```sh
cd bindings/go
go test ./descriptor/normalisation/json/v1 -run 'TestReviewPR3887_CCUtilsEdges/list-order' -count=1 -v
```

Expected result on PR head `37f1c285db0dab725bc66517751a7f35a38798d5`: FAIL with the key-order byte mismatch.
The same mismatch was reproduced on `c1b9b474faa21b1b8c59e7312afaba937350c01d`.
The merge base has no json/v1 package; this is a new-algorithm compatibility gap.
This is an offline normalisation test, not an end-to-end cryptographic signature test.

## Independent expected output

The input and expected bytes are in
`bindings/go/descriptor/normalisation/json/v1/testdata/review-pr3887-list-order/`.
Expected bytes were independently regenerated using unmodified `ocm/__init__.py`
and `ocm/sign.py` from gardener/cc-utils at
`1e44efecb4c6c123ae18acb3878317bba86ba904`, with Python dependencies `dacite` and `pyyaml`.
No resource or component lookups are allowed by the generator.

To regenerate from a checkout of that revision, with those Python dependencies installed:

```sh
CCUTILS_DIR=/path/to/cc-utils python3 .review/pr-3887-list-order/ccutils_digest.py \
  bindings/go/descriptor/normalisation/json/v1/testdata/review-pr3887-list-order/input.json > /tmp/pr3887-expected.json
cmp /tmp/pr3887-expected.json bindings/go/descriptor/normalisation/json/v1/testdata/review-pr3887-list-order/expected.json
```

The comparison succeeds; the Go test fails. Evidence only adds files and does not modify PR code.
