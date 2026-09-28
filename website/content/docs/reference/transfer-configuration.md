---
title: "Transfer Configuration"
description: "Complete reference for OCM transfer configuration: transfer settings, uploader configurations, resource matching, the HTTP streaming uploader, CEL target URLs, schema, and field descriptions."
icon: "🚚"
weight: 7
toc: true
---

This page is the technical reference for OCM transfer configuration. For a
task-oriented walkthrough of routing resources to a custom upload target, see the
[Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}})
tutorial. For the conceptual model, see
[Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}).

## Configuration Types

Transfer behaviour is controlled by two configuration types embedded in the
standard OCM configuration file. Both are carried as entries inside the central
`generic.config.ocm.software/v1` configuration and may appear together:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
    method: PUT
```

| Type                                                  | Purpose                                                                  |
|-------------------------------------------------------|--------------------------------------------------------------------------|
| `transfer.config.ocm.software/v1alpha1`               | Global transfer settings: recursion and which resources are copied.      |
| `oci.uploader.transfer.config.ocm.software/v1alpha1`  | Per-match rule that uploads a resource as a separate OCI artifact.       |
| `http.uploader.transfer.config.ocm.software/v1alpha1` | Per-match rule that streams a resource to a custom HTTP target.          |

By default the CLI looks for configuration in `$HOME/.ocmconfig`. Pass
`--config <file>` to use a different file. The corresponding CLI flags
(`--recursive`, `--copy-resources`) override the transfer config when set.

## Transfer Settings

The `transfer.config.ocm.software/v1alpha1` type controls the global transfer
behaviour. All fields are optional; when omitted they resolve to their defaults.

### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/Config.schema.json" >}}

### Fields

| Field        | Type        | Default            | Description                                                                     |
|--------------|-------------|--------------------|---------------------------------------------------------------------------------|
| `recursive`  | `-1` or `0` | `0` (no recursion) | `-1` transfers the whole reference tree; `0` transfers only the named version.  |
| `copyMode`   | enum        | `localBlob`        | Which resources are copied. See [Copy Mode](#copy-mode).                        |

#### Copy Mode

| Value          | Meaning                                                                                                               |
|----------------|-----------------------------------------------------------------------------------------------------------------------|
| `localBlob`    | Copy only resources already stored as local blobs. External resources keep their original access and are not fetched. |
| `allResources` | Fetch every external resource and re-upload it to the target. Equivalent to the CLI `--copy-resources` flag.          |

## Uploader Configurations

An **uploader configuration** routes resources that match a rule to a custom
target instead of the default download-and-embed path. Two config types are
available:

- `oci.uploader.transfer.config.ocm.software/v1alpha1` — uploads a matched
  resource as a separate OCI artifact in the target registry (or a custom
  registry when `imageReference` is set).
- `http.uploader.transfer.config.ocm.software/v1alpha1` — streams a matched
  resource's content to a custom HTTP endpoint.

Each entry is an independent rule; you may declare several.

During transfer, uploaders are evaluated in declaration order. The **first**
uploader whose `match` applies to a resource and whose applicability rules are
met wins. An uploader runs regardless of `copyMode`. If a matched uploader does
not apply to a resource (e.g. an OCI uploader without `imageReference` when the
target is a CTF), the loop continues with the next uploader. A resource with no
matching or applicable uploader follows the default `copyMode` handling (local
blob). Because matching is first-match, declare more specific rules before
broader ones.

### `oci.uploader.transfer.config.ocm.software/v1alpha1`

Uploads a matched resource as a separate OCI artifact in the target registry.
The resource is stored independently from the component version, making it
directly addressable and pullable with standard OCI tools.

#### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/OCIUploaderConfig.schema.json" >}}

#### Fields

| Field                 | Type                | Required | Description                                                                                                                                          |
| --------------------- | ------------------- | -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `match`               | object              | no       | Restricts the resources this uploader applies to. When omitted, every resource matches.                                                              |
| `match.accessType`    | `runtime.Type`      | no       | Access type to match (by name; omitted version = any). Optional for the OCI uploader (required for HTTP). Does not resolve aliases — see note below. |
| `match.name`          | string              | no       | Restrict the match to resources with this exact name.                                                                                                |
| `match.version`       | string              | no       | Restrict the match to resources with this exact version.                                                                                             |
| `match.extraIdentity` | `map[string]string` | no       | Restrict the match to resources whose identity contains these key/value pairs.                                                                       |
| `imageReference`      | CEL expression      | no       | Target image reference. A `${…}` CEL template or a plain literal. When empty, the default mapping is used.                                           |

{{< callout context="note" title="match.accessType does not resolve aliases" icon="outline/info-circle" >}}
`match.accessType` compares type names exactly. Access types often have
multiple alias names in descriptors (e.g. OCI images appear as `OCIImage`,
`ociArtifact`, `ociRegistry`, or `ociImage`; local blobs as `LocalBlob` or
`localBlob`). If you need to match by access type, add one uploader entry per
alias name your descriptors carry. Matching by `match.name` avoids this issue
entirely.
{{< /callout >}}

#### Applicability

Not every resource type can be uploaded as an OCI artifact. The following table
shows which access types the OCI uploader supports:

| Source access type | Applies when | `referenceName` |
| -------------------- | -------------- | ----------------- |
| `OCIImage` (all aliases) | always | `repository[:tag]` (registry and digest dropped from `imageReference`) |
| `Helm` | always | chart repository URL path, chart name and version (e.g. `podinfo/podinfo:6.5.0` for repository `https://stefanprodan.github.io/podinfo` and chart `podinfo:6.5.0`) |
| `LocalBlob` (OCI manifest media type) | media type is an OCI-compliant manifest | `access.referenceName` verbatim (may be empty) |
| anything else (Wget, S3, GitHub, …) | never | — (falls through) |

#### Default mapping (no `imageReference`)

Without `imageReference`, the uploader applies only when:

1. The component target is an OCI registry, and
2. The resource has a non-empty `referenceName`.

The image reference is then `targetRepository + "/" + referenceName`, where
`targetRepository` is the target registry's `BaseUrl` plus an optional `SubPath`.
This is the same mapping that the former `--upload-as ociArtifact` flag produced.
If either condition is not met, the uploader falls through to the next uploader
or to the default local blob handling.

#### Custom `imageReference`

With `imageReference`, the value is a CEL template (`${…}`) or a plain literal.
Three aliases are available:

| Alias              | Value                                                                          |
| ------------------ | ------------------------------------------------------------------------------ |
| `resource`         | The source resource descriptor (same as the HTTP uploader's `resource` alias). |
| `referenceName`    | The derived reference name for the resource. Only offered when non-empty.      |
| `targetRepository` | The target repository path. Only offered for OCI registry targets.             |

A custom `imageReference` also works for CTF targets, because `TransferOCIArtifact`
and `AddOCIArtifact` push to the specified image reference independently of the
component target.

#### Examples

Upload every applicable resource as an OCI artifact using the default mapping
(with `--copy-resources`, equivalent to the former `--upload-as ociArtifact`):

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
```

Relocate images to a custom registry path:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  imageReference: '${"ghcr.io/mirror/" + referenceName}'
```

Upload a specific resource to a fixed reference:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match:
    name: my-image
  imageReference: ghcr.io/target-org/special/my-image:1.0.0
```

See [Migrate from --upload-as to Uploader Configurations]({{< relref "docs/how-to/migrate-from-upload-as.md" >}}).

### `http.uploader.transfer.config.ocm.software/v1alpha1`

Streams a matched resource's content directly to an HTTP endpoint (typically a
`PUT` upload) and rewrites the resource to a `Wget/v1` access pointing at the
uploaded location. The **source** may be any access type (wget, OCI, S3, GitHub,
…) — its content is fetched through the access-type-specific downloader; only the
**target** is always an HTTP endpoint. The source content is piped straight into
the request body, so it is never buffered in memory or on disk. The digest is
computed during the stream, or — when the source resource already carries one —
verified as the bytes pass through.

The upload request and the published access are kept separate. `method`, `header`,
`body` and `noRedirect` describe the **upload request** only. The **published**
`Wget/v1` access — the download access recorded on the transferred resource —
carries just the resolved `url` and `mediaType`, never the write verb, body or
request headers. This ensures a later `ocm download` issues a plain read (GET) and
cannot re-send the write request that would overwrite the uploaded object.

#### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/HTTPUploaderConfig.schema.json" >}}

#### Fields

`targetURL` and `mediaType` also become the published `Wget/v1` download access;
`method`, `header`, `body` and `noRedirect` apply to the upload request only:

| Field                 | Type                  | Applies to                        | Description                                                                            |
|-----------------------|-----------------------|-----------------------------------|----------------------------------------------------------------------------------------|
| `match.accessType`    | `runtime.Type`        | —                                 | Access type this uploader applies to (matched by name; omitted version = any).         |
| `match.name`          | string (optional)     | —                                 | Restrict the match to resources with this exact name.                                  |
| `match.version`       | string (optional)     | —                                 | Restrict the match to resources with this exact version.                               |
| `match.extraIdentity` | `map[string]string`   | —                                 | Restrict the match to resources whose identity contains these key/value pairs.         |
| `targetURL`           | CEL expression        | request + published (`url`)       | The upload URL; also the published download URL. See CEL Expressions below.            |
| `method`              | string                | request (`verb`)                  | HTTP method for the upload request. Defaults to PUT. Not on the published access.      |
| `header`              | `map[string][]string` | request                           | HTTP headers sent with the upload request. May be CEL-templated. Request only.         |
| `noRedirect`          | bool                  | request                           | Disable following HTTP redirects on the upload. Not on the published access.           |
| `mediaType`           | string                | request + published (`mediaType`) | Media type recorded on the resource. Defaults to the source's.                         |

### Routing Resources to Different Targets

Because a rule can match on identity as well as access type, several resources of
the **same** access type can be routed to **different** targets. List the specific
rules first; a final rule without `name`/`version`/`extraIdentity` acts as a catch-all:

```yaml
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
  # Docs go to the docs bucket.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
      name: docs
    targetURL: '${"https://docs.example.com" + url(resource.access.url).path}'
  # Everything else Wget goes to the generic bucket.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://blobs.example.com/" + resource.name + "/" + resource.version}'
```

### CEL Expressions

`targetURL` and every `header` value are [CEL](https://cel.dev/) expressions — the
same expression language the transfer graph uses to resolve every other field. A
CEL value **must be wrapped in `${…}`**, matching how every other CEL field is
written in the transfer graph. It is evaluated against the source resource, exposed
under the `resource` alias, and resolved by the transfer runtime, so the produced
plan is deterministic. CEL string concatenation (`+`), conditionals (`cond ? a : b`),
and comparisons are all available. Append any static query string inside the
expression. (A `header` value with no `${…}` is a plain literal and is sent verbatim.)

The `resource` alias always exposes:

| Expression                               | Value                                                        |
|------------------------------------------|--------------------------------------------------------------|
| `resource.name`                          | Resource name.                                               |
| `resource.version`                       | Resource version.                                            |
| `resource.type`                          | Resource type.                                               |
| `resource.extraIdentity.<key>`           | A value from the resource's extra identity.                  |
| `resource.labels`                        | The resource labels as a list of `{name, value}` objects.    |
| `resource.digest.value`                  | The source digest value, when the resource carries a digest. |
| `resource.digest.hashAlgorithm`          | The source digest hash algorithm (e.g. `SHA-256`).           |
| `resource.digest.normalisationAlgorithm` | The source digest normalisation algorithm.                   |

Every field of the **source access** is exposed dynamically under
`resource.access.<field>`, so the expression works with any access type — the
field names are exactly those of that access. For example:

| Source access | Available under `resource.access` |
|---------------|-----------------------------------|
| `Wget/v1`     | `url`, `mediaType`                |
| `OCIImage/v1` | `imageReference`                  |
| `S3/v1`       | `bucket`, `key`, `region`, …      |

To decompose a URL-bearing access into its parts, use the inbuilt `url()` CEL
function (CEL has no URL parser). `url(<string>)` (also callable as
`<string>.url()`) parses a URL string and returns a map with the string keys
`scheme`, `host`, `hostname`, `port`, `path`, `rawPath`, `rawQuery`, `fragment`,
and `user`. For a `Wget/v1` source, `url(resource.access.url).path` yields the
source URL's path. Because the field set is derived from the matched resource's
own access, an expression may only reference fields that exist on every resource
the uploader matches — scope the rule with `match.accessType` so all matched
resources share a shape.

Further inbuilt functions help build checksum headers:
`contentDigestAlgorithm(<string>)` maps an OCM digest algorithm name to its RFC 9530
`Content-Digest`/`Repr-Digest` key (`SHA-256` → `sha-256`); `hex.decode`/`hex.encode`
and the CEL encoders extension's `base64.encode`/`base64.decode` convert a hex digest
to the base64 value those fields expect. See [Templating Headers](#templating-headers).

Examples:

```yaml
# Preserve the source path under a new host
targetURL: '${"https://mytarget.example.com" + url(resource.access.url).path}'

# Route by name and version
targetURL: '${"https://cdn.example.com/" + resource.name + "/" + resource.version + "/blob"}'

# Use extra identity attributes
targetURL: '${"https://" + resource.extraIdentity.region + ".example.com/" + resource.extraIdentity.arch + url(resource.access.url).path}'

# Conditional target driven by an extra-identity attribute
targetURL: '${resource.extraIdentity.tier == "public" ? "https://cdn.example.com" + url(resource.access.url).path : "https://internal.example.com" + url(resource.access.url).path}'

# Non-wget source: reference an access-specific field (OCI imageReference)
targetURL: '${"https://mirror.example.com/" + resource.access.imageReference}'
```

Referencing a field that is absent at execution time fails the transfer with a
clear error rather than producing a partial URL.

#### Templating Headers

`header` values are templated with the same `${…}` CEL expressions. This is how
you forward a checksum the source already advertised on the upload request. Header
expressions read `resource.digest`, which is only present when the source resource
carries a digest (e.g. pinned from the source via the checksum-http configuration),
so scope the rule with `match` so every matched resource has one. A value without
`${…}` is sent as a literal.

`resource.digest.value` is the **hex** digest and `resource.digest.hashAlgorithm`
is the OCM algorithm name (e.g. `SHA-256`). Two inbuilt CEL functions build a
strictly conformant
[`Content-Digest`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Digest)
/ `Repr-Digest` field (RFC 9530):

- `contentDigestAlgorithm(<name>)` maps the OCM algorithm name to the RFC 9530 key,
  lower-cased and canonicalized (`SHA-256` → `sha-256`, `SHA-512` → `sha-512`,
  `MD5` → `md5`, and `SHA-1` → the registered key `sha`). Matching is case- and
  separator-insensitive; an unknown algorithm fails the transfer.
- RFC 9530 carries the digest **value** as base64, while `resource.digest.value` is
  hex, so convert it with `base64.encode(hex.decode(resource.digest.value))`. The
  `base64.encode`/`base64.decode` functions come from the CEL
  [encoders extension](https://github.com/google/cel-go/blob/master/ext/README.md);
  `hex.encode`/`hex.decode` are inbuilt companions.

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://mytarget.example.com/uploads" + url(resource.access.url).path}'
    method: PUT
    header:
      # RFC 9530 Content-Digest: sha-256=:<base64>:
      Content-Digest: ['${contentDigestAlgorithm(resource.digest.hashAlgorithm) + "=:" + base64.encode(hex.decode(resource.digest.value)) + ":"}']
      # A simple non-standard checksum header carrying the raw hex value.
      X-Checksum-Sha256: ['${resource.digest.value}']
      # A static literal header is sent verbatim (no ${...}).
      X-Uploaded-By: ['ocm-transfer']
```

##### Example: JFrog Artifactory

[Artifactory](https://jfrog.com/help/r/jfrog-artifactory-documentation) verifies
uploads against client-supplied checksum headers and takes the **hex** digest
directly (no RFC 9530 base64), so `resource.digest.value` maps straight onto its
`X-Checksum-*` family. With the default checksum policy Artifactory requires a
checksum header on deploy:

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    # Deploy under <repo>/<path>; here the source URL path is reused.
    targetURL: '${"https://myorg.jfrog.io/artifactory/my-repo" + url(resource.access.url).path}'
    method: PUT
    header:
      # Artifactory verifies the uploaded bytes against this hex SHA-256.
      X-Checksum-Sha256: ['${resource.digest.value}']
```

To link an artifact that Artifactory **already** stores without re-uploading the
body (["Deploy Artifact by Checksum"](https://jfrog.com/help/r/jfrog-rest-apis/deploy-artifact-by-checksum)),
set `X-Checksum-Deploy: true` alongside the checksum. Artifactory returns `201` when
it finds a matching artifact and `404` when the content must still be uploaded:

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://myorg.jfrog.io/artifactory/my-repo" + url(resource.access.url).path}'
    method: PUT
    header:
      X-Checksum-Deploy: ['true']
      X-Checksum-Sha256: ['${resource.digest.value}']
```

Both require the source resource to carry a SHA-256 digest (`resource.digest.hashAlgorithm == "SHA-256"`).

#### Credentials

The uploader resolves credentials for the **target** URL independently from the
source resource, using the target host's
[consumer identity]({{< relref "docs/reference/credential-consumer-identities.md" >}})
(the same `Wget` identity used for downloads). Configure target-side credentials
the same way you would for any HTTP endpoint; see
[Credential Types]({{< relref "docs/reference/credential-types.md" >}}).

## Notes

### Precedence

For a given resource, uploaders are evaluated in declaration order and the first
that both matches and applies wins. An uploader runs regardless of `copyMode`. If
a matched uploader does not apply, the loop continues. A resource with no
matching or applicable uploader follows the default `copyMode` handling (local blob).

### Deterministic Plans

Transfer produces a deterministic transformation plan: components are processed in
sorted order and transformation identifiers are derived from stable hashes. The
plan is rendered with human-readable labels such as
`my-app@1.0.0 [Stream icons to mytarget.registry.com]`.

## Related Documentation

- [Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — tutorial that walks through an uploader end to end
- [Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — the conceptual transfer model
- [Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) — the `Wget/v1` type produced by the HTTP streaming uploader
- [HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) — tuning the HTTP client used for the upload
