# Resource download verification PoC

This experiment builds on [PR #3641](https://github.com/open-component-model/open-component-model/pull/3641),
pinned at `a423aea3bb205c4abe4bfc2f534a6aba5640753e`.

## Architecture: compose at the plugin boundary

The CLI already obtains a `resource.Repository` from
`ResourcePluginRegistry.GetResourcePlugin`. The registry now returns a facade
combining the selected downloader and a verifier provider. Neither the CLI nor
each individual repository constructor needs to remember to attach verification.

```text
CLI / controller / other plugin consumers
    GetResourcePlugin(access)
        -> built-in repository OR external-plugin converter
        -> shared verification facade
    GetResourceCredentialConsumerIdentity(resource)
        -> unchanged forwarding to selected repository
    credentialGraph.Resolve(identity)
        -> unchanged credential resolution
    DownloadResource(resource, credentials)
        -> provider validates and snapshots expected digest
        -> selected repository downloads content
        -> verifier wraps downloaded content
    consume blob through EOF and check errors
```

Both branches of plugin lookup are wrapped. Merely changing
`ResourceRegistry.DownloadResource` would not be sufficient: the CLI obtains a
plugin and invokes its methods directly.

The default facade handles generic blob verification on the host side. For an
external plugin, its existing `GetGlobalResource` RPC returns a `Location`; the
existing converter creates a blob, and the facade then attaches verification.
The external plugin does not need a new RPC or a claim that its output is verified.

The facade forwards upload and credential-identity calls unchanged. It preserves
`OwnershipAwareRepository` and `SBOMDiscoverer` only when the selected repository
supports them, including the combination of both capabilities.

## Interfaces and packaging

- `blob/verification`: byte-level streaming verification against an independent
  digest, without resource or plugin dependencies.
- `repository/resource_verification.go`: `ResourceVerifierProvider` selects and
  prepares a `ResourceVerifier` before transport is invoked. The generic provider
  interprets descriptor digests and binds the expected digest to a verifier.
- `plugin/manager/registries/resource/verification.go`: the private facade combines
  the existing `resource.Repository` with that provider.
- `plugin/manager/registries/resource/registry.go`: installs the facade on both
  built-in and external lookup paths.

This follows the credentials architecture: domain interfaces describe behavior,
providers supply implementations, and shared orchestration uses those interfaces.
There is no second `ResourceBackend` hierarchy and no per-technology public facade.
GitHub, S3, and wget implementations are transport implementations again.

Verifier contracts:

```go
type ResourceVerifierProvider interface {
    GetResourceVerifier(context.Context, *descriptor.Resource) (ResourceVerifier, error)
}

type ResourceVerifier interface {
    Verify(context.Context, blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error)
}
```

Custom normalization strategies can be supplied through the provider interface.
A future normalization-aware registry can implement it without changing the CLI.
An external verifier plugin capability is not implemented by this PoC.

## Policy configuration

The registry defaults to `VerifyIfPresent`: missing digests and explicit exclusions
pass through with a warning. Malformed digests and unsupported normalization fail
before transport, even under this compatibility policy.

Strict callers configure the registry rather than every backend:

```go
registry := resource.NewResourceRegistry(ctx,
    resource.WithResourceVerifierProvider(
        repository.NewGenericResourceVerifierProvider(repository.RequireDigest),
    ),
)
```

Then registration and lookup are unchanged:

```go
if err := registry.RegisterInternalResourcePlugin(backend); err != nil {
    return err
}
repo, err := registry.GetResourcePlugin(ctx, res.Access)
if err != nil {
    return err
}
content, err := repo.DownloadResource(ctx, res, credentials)
```

The consumer sees only `resource.Repository`; it never calls a verifier itself.
Nil providers and nil selected verifiers fail closed. The generic provider captures
an immutable expected digest before the downloader can mutate the resource.

## Guarantees and prototype boundaries

- Verification is streaming, not eager. Successful `DownloadResource` means a
  verifying reader is installed; consumers must read through EOF and check errors
  before publishing or trusting the output. Partial reads fail on close.
- `blob.Copy` probes EOF after the advertised size, allowing exact-size successful
  verification while rejecting trailing content without writing the extra bytes.
- Temporary blob ownership and close behavior remain forwarded through the
  verifying blob. Consumers must discard output already written on failure.
- Missing digests are never represented as verified. Authenticating the expected
  digest through descriptor signatures remains a separate concern.
- **The default provider supports only generic blob normalization** (and empty
  normalization for legacy compatibility). A declared OCI-specific or other
  unsupported normalization is rejected before download. This intentionally
  changes those plugin-mediated downloads: it is not a production-ready universal
  verifier, and there is no silent OCI bypass. Add the corresponding provider
  strategy before using this prototype for such resources.
- Local resources use `GetLocalResource` on component repositories and are outside
  this facade. Sources and directly invoked backend implementations are outside
  it too. The guarantee belongs to the resource plugin lookup boundary.
- Digest establishment calls transport directly, below the facade, and compares
  any supplied expectation in its digest processor. It does not recursively invoke
  registry-based download verification.
- The registry option is available to Go callers. Exposing strict policy through
  CLI configuration and composing technology-specific strategies are follow-ups.
- Go interfaces cannot prove arbitrary verifier implementations are correct. The
  prototype enforces invocation of the configured verifier, not plugin honesty.
- `VerifyDownload` remains as a compatibility helper; backends no longer call it.

## Tests and review

Registry tests cover built-in and converted external plugins, matching/mismatching
content, pre-fetch rejection, policy, digest mutation, credential forwarding,
upload forwarding, backend errors, and every ownership/SBOM capability combination.
External tests return a local-file location and prove host-side mismatch detection.
The existing subprocess plugin fixture also exercises lookup through the facade.

Backend verification tests now go through registry registration and lookup; raw
backend tests explicitly show that direct transport calls do not verify resource
digests. Provider and blob tests exercise the underlying pieces independently.

Focused validation from `bindings/go`:

```sh
go test -short ./blob/... ./repository/... ./plugin/manager/registries/resource/... ./github/... ./s3/repository/... ./wget/repository/... ./cli/cmd/download/... ./cli/internal/plugin/builtin/...
```

Repository-wide checks:

```sh
task tools:lint
task bindings/go:test
```

The fork draft targets a snapshot of PR #3641, keeping its review diff limited to
this experiment. The original author's branch is untouched.
