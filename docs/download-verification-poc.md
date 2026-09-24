# Resource download verification PoC

This experiment builds on [PR #3641](https://github.com/open-component-model/open-component-model/pull/3641),
pinned at `a423aea3bb205c4abe4bfc2f534a6aba5640753e`.

## Packaging

All reusable resource-verification logic lives in `bindings/go/repository/verify`:

- `NewResourceRepository`: a facade over `repository.ResourceRepository`.
- `ResourceVerifierProvider` and `ResourceVerifier`: optional technology-specific
  verification supplied by the wrapped repository.
- `WithFallbackResourceVerifierProvider`: configures the generic fallback.
- Digest parsing, missing-digest policy, and the generic implementation.

This package imports neither the plugin manager nor the CLI. Low-level byte
verification remains in `blob/verification`, with no resource dependencies.

The plugin registry only exposes a generic `SetRepositoryDecorator` hook. It has
no verification imports, policies, or automatic verification defaults. Only CLI
plugin registration (`cli/internal/plugin/builtin.Register`) installs verification:

```go
manager.ResourcePluginRegistry.SetRepositoryDecorator(
    func(base resource.Repository) resource.Repository {
        return verify.NewResourceRepository(base)
    },
)
```

The hook adapts both built-in repositories and converted external plugins at
lookup time, including plugins registered before the hook was configured. Merely
wrapping the built-in registrations would leave external plugins unprotected.
It does not change already-returned handles. Setup should finish before lookups.

## Repository-provided verification, generic fallback otherwise

```text
CLI obtains resource.Repository through GetResourcePlugin
    -> generic registry decorator hook
    -> verify.NewResourceRepository(selected downloader)
        -> repository implements verify.ResourceVerifierProvider? use it
        -> otherwise use generic fallback

CLI calls DownloadResource(resource, credentials)
    -> selected provider validates and snapshots expectation
    -> wrapped repository downloads content
    -> selected verifier verifies or wraps the content
```

The repository can implement `GetResourceVerifier` alongside its existing download
methods. It does not need to invoke verification itself: the facade owns that.

```go
type ResourceVerifierProvider interface {
    GetResourceVerifier(context.Context, *descriptor.Resource) (ResourceVerifier, error)
}

type ResourceVerifier interface {
    Verify(context.Context, blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error)
}
```

A provider error, nil verifier, or verification failure is returned to the caller,
**never retried through the fallback**. Fallback means absent capability, not failed
verification. Providers must capture the expectation before transport can mutate
it. Verifiers own their input, including cleanup on failure.

Uploads and credential identity calls are forwarded unchanged. Typed credentials
reach the selected downloader unchanged. The facade preserves optional
`OwnershipAwareRepository` and `SBOMDiscoverer` capabilities only when supported.

## Standalone library usage and policy

Libraries can use the facade without any plugin infrastructure:

```go
repo := verify.NewResourceRepository(backend,
    verify.WithFallbackResourceVerifierProvider(
        verify.NewGenericResourceVerifierProvider(verify.RequireDigest),
    ),
)
content, err := repo.DownloadResource(ctx, res, credentials)
```

The default generic fallback is `VerifyIfPresent`: missing digests and explicit
exclusions pass through with a warning. Malformed digests and unsupported
normalization fail before fetching. `RequireDigest` rejects missing expectations.

Fallback options do not override repository-provided verifiers. Specialized
providers own their own policies; a globally enforced strict policy independent
of verifier choice is outside the prototype. A nil fallback is permitted when the
repository supplies a provider, otherwise it fails closed.

## External plugins

External plugins retain the existing `GetGlobalResource` RPC returning a `Location`.
The host-side converter creates a blob; CLI-installed decoration applies the
facade and generic verification to it. No plugin verification claim is trusted in
place of the host check.

External adapters currently do not expose the optional provider interface. A
specialized external verifier requires a future capability/wire-contract extension.

## Boundaries

- Generic verification is streaming. Consumers must read to EOF and check errors
  before trusting or publishing content; partial reads fail on close. Specialized
  verifiers may instead verify eagerly.
- `blob.Copy` probes EOF after exact-size copies so verification completes and
  trailing bytes are rejected without writing them. Failed output must be discarded.
- Only generic blob normalization (plus legacy empty normalization) is implemented
  by the fallback. No production OCI verifier exists yet: OCI-specific normalization
  fails unless its repository supplies an appropriate provider. No silent bypass.
- CLI plugin registration enables verification. A bare resource registry or plugin
  manager does not. Other applications, including the controller, must explicitly
  install the decorator or construct the facade to obtain this guarantee.
- Local resources (`GetLocalResource`), sources, and directly invoked raw backends
  are outside the facade. Digest establishment intentionally uses raw transport.
- Authenticating the expected digest through signatures is a separate concern.
- Go interfaces enforce orchestration, not the correctness of a custom verifier.

## Examples and validation

`plugin/manager/registries/resource/verification_test.go` contains a concrete
repository-provided whitespace-normalized SHA-256 verifier. It demonstrates custom
normalization, original-byte preservation, mismatch rejection, provider precedence,
and no fallback after errors. These integration tests explicitly install decoration.

`repository/verify/resource_repository_test.go` tests the reusable facade without
plugins: forwarding, policies, ordering, snapshot expectations, and optional
capabilities. `cli/internal/plugin/builtin/builtin_test.go` demonstrates that a raw
registry becomes verification-aware through CLI registration and still prefers a
repository-provided verifier.

From `bindings/go`:

```sh
go test -short ./repository/... ./plugin/manager/registries/resource ./cli/internal/plugin/builtin/... ./cli/cmd/download/... ./github/repository/resource ./s3/repository ./wget/repository
```

Repository-wide checks:

```sh
task tools:lint
task bindings/go:test
```

The fork draft targets a snapshot of PR #3641, keeping the review diff limited to
this experiment. The original author's branch is untouched.
