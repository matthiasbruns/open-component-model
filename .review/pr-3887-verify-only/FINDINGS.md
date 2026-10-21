# PR 3887: verification-only guard blocks normalized-pin verification

Tested PR head: `59be925ea3c8a0d377fffeeb26bb9e63f2398db4`.

## Confirmed finding

The new guard at `bindings/go/signing/digest.go:136` rejects
`jsonNormalisation/v1` in `GenerateDigest`. That helper is also used by
`repository.verifyNormalizedPin` (`repository/pin.go:175`), a read-only
verification path. The same descriptor and digest pass
`signing.VerifyDigestMatchesDescriptor` and then fail pin verification with
`normalisation algorithm is supported for verification only, not signing`.

The test computes a digest independently of GenerateDigest, first proves it
passes signature-digest verification, then asserts it also passes the actual
normalized-pin verification helper. It includes a passing v4alpha1 control.

## Reproduce

From the repository root:

```sh
cd bindings/go
go test ./repository -run TestReviewPR3887_VerifyOnlyPin -count=1 -v
```

Expected: both subtests pass. Actual on the tested head: v4alpha1 passes; v1
fails with ErrSigningUnsupported. Exit status 1.

A temporary Go compiler overlay removing only this guard makes both subtests
pass (exit 0). This isolates the cause, and is not a proposed complete fix:
actual signing must remain prohibited. No production files were edited.

The same test on merge base `f280eca271158c021577b4958ffdc00c892a8e10`
passes for v4alpha1 but fails earlier for v1 with unknown normalisation
algorithm. This is incomplete integration of new v1 support, not a regression
in previously working base-branch v1 verification.

## Impact and suggested change

CLI component verification reaches this helper via GetComponentVersionByArtifact.
The controller also calls GenerateDigest from workerpool.compareDigest
(`kubernetes/controller/internal/resolution/workerpool/workerpool.go:519`)
when checking a child against the parent reference digest. That controller
path was inspected but was not exercised end to end.

Keep the signing prohibition at actual signing boundaries, or migrate read-only
digest comparison callers to a verification API. Preserve mismatch detection.

## Scope and limits

Only the pin finding is published here. The test is a focused helper-level
regression test, not a live registry, CLI or Kubernetes end-to-end test. It
does not test cryptographic RSA/PSS interoperability.

The PR covers part of ocm-project#1367: cc-utils v1 verification, with documented
differences from legacy OCM Go. It does not close the full compatibility issue.

Go lint and Markdown lint passed with the added evidence. The focused gate
`task bindings/go:test -- -run 'Test.*(Normalis|Normalize|Digest|Convert.*Label)'`
also passed. The intentionally failing review test is excluded by that gate's
test-name filter and run separately above.
