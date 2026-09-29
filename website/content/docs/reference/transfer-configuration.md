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
    recursive: -1
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
    method: PUT
```

| Type                                                                     | Purpose                                                                   |
|--------------------------------------------------------------------------|---------------------------------------------------------------------------|
| `transfer.config.ocm.software/v1alpha1`                                  | Global transfer settings: recursion depth.                                |
| `oci.uploader.transfer.config.ocm.software/v1alpha1`                     | Per-match rule that uploads a resource as a separate OCI artifact.        |
| `http.uploader.transfer.config.ocm.software/v1alpha1`                    | Per-match rule that streams a resource to a custom HTTP target.           |
| `localblob.uploader.transfer.config.ocm.software/v1alpha1`               | Per-match rule that downloads a resource and embeds it as a local blob.   |
| `reference.uploader.transfer.config.ocm.software/v1alpha1`               | Per-match rule that keeps a resource by reference (no transformation).    |

By default the CLI looks for configuration in `$HOME/.ocmconfig`. Pass
`--config <file>` to use a different file. `--recursive` overrides the transfer
config when set.

### Deprecated Flags

The `--copy-resources` and `--upload-as` flags are deprecated. They are
translated into uploader configuration entries appended after all configured
entries, and the CLI logs a warning with the equivalent configuration. See the
[migration guide]({{< relref "docs/how-to/migrate-from-upload-as.md" >}}) for
details.

## Transfer Settings

The `transfer.config.ocm.software/v1alpha1` type controls the global transfer
behaviour. All fields are optional; when omitted they resolve to their defaults.

### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/Config.schema.json" >}}

### Fields

| Field        | Type        | Default            | Description                                                                     |
|--------------|-------------|--------------------|---------------------------------------------------------------------------------|
| `recursive`  | `-1` or `0` | `0` (no recursion) | `-1` transfers the whole reference tree; `0` transfers only the named version.  |

## Uploader Configurations

An **uploader configuration** routes the resources it selects to a custom
target instead of the default handling. Four config types are available:

- `oci.uploader.transfer.config.ocm.software/v1alpha1` — uploads a selected
  resource as a separate OCI artifact in the target registry (or a custom
  registry when `imageReference` is set).
- `http.uploader.transfer.config.ocm.software/v1alpha1` — streams a selected
  resource's content to a custom HTTP endpoint.
- `localblob.uploader.transfer.config.ocm.software/v1alpha1` — downloads a
  selected resource and embeds it in the target as a local blob.
- `reference.uploader.transfer.config.ocm.software/v1alpha1` — keeps a selected
  resource by reference (no transformation; access unchanged in the target).

Each entry is an independent rule; you may declare several.

### Selection

For each resource, uploaders are evaluated in declaration order. An uploader
**selects** a resource when its `match` evaluates to `true`. An omitted `match`
means the uploader type's default match; the HTTP uploader has no default and
requires `match`.

The **first** uploader that selects a resource handles it. There is no
fall-through: if the selected uploader cannot handle the resource (for example
an OCI uploader selecting a `Wget` resource, or an `imageReference` that does
not evaluate for the resource), the transfer fails with an error that names the
uploader and the resource.

A resource that no uploader selects follows the baseline: local blobs are
copied as local blobs; all other resources stay by reference (their access is
unchanged in the target). A catch-all
`localblob.uploader.transfer.config.ocm.software/v1alpha1` entry copies
every supported resource. Declare more specific rules before broader ones.

#### `match`

`match` is a plain CEL boolean expression (not wrapped in `${…}`). It is
evaluated once per resource while the transfer graph is built. The following
identifiers are available:

| Identifier | Value |
| --- | --- |
| `resource` | The source resource, dynamically typed, so `has(resource.access.<field>)` and field reads compile for every access type. |
| `target` | The transfer target as a map. An OCI registry is `{"type": "OCIRepository", "baseUrl": …, "subPath": …}`; a CTF archive is `{"type": "CommonTransportFormat", "filePath": …}`. |

Available functions:

| Function | Description |
| --- | --- |
| `resource.access.isType(string)` | True when the resource's access type matches the argument, with alias and version resolution (see below). |
| `resource.access.isType(list(string))` | True when the resource's access type matches **any** element in the list. |
| `isOCIManifest(string)` | True for OCI image manifest and index, and Docker manifest and manifest list media types. |
| `toOCI()` | Returns a map with keys `host`, `registry`, `repository`, `tag`, `digest`, `reference` for OCI image accesses. |
| `url(string)` | Parses a URL and returns a map with keys `scheme`, `host`, `hostname`, `port`, `path`, `rawPath`, `rawQuery`, `fragment`, `user`. |
| String extensions | `split`, `join`, `endsWith`, `startsWith`, `contains`, `replace`, `trim`, … from the [CEL string extensions](https://github.com/google/cel-go/blob/master/ext/README.md). |

##### `isType` semantics

`resource.access.isType(arg)` receives the resource's access map and tests
its `type` field against the argument. Both sides are resolved through the
transfer access scheme (alias resolution):

- **Unversioned argument**: matches any version. `isType("OCIImage")` matches
  `ociArtifact/v1`, `ociImage/v1`, `ociRegistry/v1`, etc.
- **Versioned argument**: the resolved types must be equal, so aliases of the
  same version match (`isType("ociImage/v1")` matches `ociArtifact/v1`), but
  `isType("S3/v1")` does not match `s3/v2`.
- **Unregistered types** (for example `Custom/v1`) resolve to themselves:
  `isType("Custom")` matches `Custom/v1` by name.

| Access `type` in descriptor | `isType("OCIImage")` | `isType("ociArtifact/v1")` | `isType("Helm")` | `isType("LocalBlob")` |
| --- | --- | --- | --- | --- |
| `ociArtifact/v1` | ✅ | ✅ | ❌ | ❌ |
| `ociImage/v1` | ✅ | ✅ | ❌ | ❌ |
| `ociRegistry/v1` | ✅ | ✅ | ❌ | ❌ |
| `localBlob/v1` | ❌ | ❌ | ❌ | ✅ |
| `helm/v1` | ❌ | ❌ | ✅ | ❌ |
| `Wget/v1` | ❌ | ❌ | ❌ | ❌ |

- Every uploader type except `http` has a default `match` (see the type
  sections). An explicit `match` **replaces** the default entirely; writing the
  default out is equivalent to omitting it.
- A `match` that does not compile, does not evaluate, or does not return a bool
  fails the transfer.

### `oci.uploader.transfer.config.ocm.software/v1alpha1`

Uploads a selected resource as a separate OCI artifact in the target registry.
The resource is stored independently from the component version, making it
directly addressable and pullable with standard OCI tools.

#### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/OCIUploaderConfig.schema.json" >}}

#### Fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `match` | CEL expression | no | A CEL boolean expression selecting the resources this uploader handles (`resource` and `target` are available; test access types with `resource.access.isType`). When omitted, the default below applies; an explicit value replaces it. |
| `imageReference` | CEL expression | no | Target image reference: a `${…}` CEL template or a plain literal. Defaults to the expression described in [`imageReference`](#imagereference). |

#### Default `match`

```yaml
match: >-
  target.type == "OCIRepository"
  && (resource.access.isType(["OCIImage", "Helm"])
    || (resource.access.isType("LocalBlob")
      && isOCIManifest(resource.access.mediaType)
      && has(resource.access.referenceName)))
```

The OCI uploader selects, on OCI registry targets only:

| Source access type | Selected when | Name used in the default `imageReference` |
| --- | --- | --- |
| `OCIImage` (all aliases) | always | `resource.access.toOCI().repository` + tag. E.g. `ghcr.io/org/image:v1` → `org/image:v1` (registry and digest dropped). |
| `Helm` | always | Helm repository URL path + chart name, tagged with version. E.g. `https://stefanprodan.github.io/podinfo`, chart `podinfo:6.5.0` → `podinfo/podinfo:6.5.0`. |
| `LocalBlob` | the media type is an OCI manifest and the access has a `referenceName` | `resource.access.referenceName` verbatim (whatever it contains, including host/port/digest). E.g. `ghcr.io/org/image:v1` → `ghcr.io/org/image:v1`. |
| anything else (Wget, S3, GitHub, …) | never | — |

This is exactly the scope of the deprecated `--upload-as ociArtifact`. An explicit
`match` may select only resources the OCI uploader can upload: OCI images, Helm
charts, and local blobs holding an OCI manifest. Selecting anything else fails
the transfer with `oci uploader cannot upload access type …` or `… not an OCI
manifest`.

#### `imageReference`

`imageReference` is a CEL template (`${…}`) or a plain literal. It sees the same
identifiers as `match`:

| Identifier | Value |
| --- | --- |
| `resource` | The source resource descriptor (same `resource` alias as the HTTP uploader). Fields are resolved dynamically (`dyn()`), so a template may use `has()` and read fields of any access type. For OCI image accesses, call `resource.access.toOCI()` to obtain a map with keys `host`, `registry` (= host), `repository`, `tag`, `digest`, `reference`. This is the same `toOCI()` function the OCM Kubernetes controller offers in its CEL expressions. In transfers, `toOCI()` resolves OCI image accesses only. |
| `target` | The transfer target. An OCI registry exposes `target.baseUrl` (the registry, including a scheme if the target has one, e.g. `http://127.0.0.1:5000`) and `target.subPath` (the repository prefix; may be `""`). A CTF archive exposes `target.filePath`. |

When `imageReference` is omitted, the uploader uses the following default:

```yaml
imageReference: |-
  ${target.baseUrl
    + (target.subPath == "" ? "" : "/" + target.subPath)
    + "/" + (has(resource.access.referenceName)
      ? resource.access.referenceName
      : has(resource.access.helmChart)
        ? (url(resource.access.helmRepository).path.split("/") + [resource.access.helmChart.split(":")[0]]).filter(s, s != "").join("/")
          + (has(resource.access.version) && resource.access.version != ""
            ? ":" + resource.access.version
            : (resource.access.helmChart.contains(":") ? ":" + resource.access.helmChart.split(":")[1] : ""))
        : resource.access.toOCI().repository
          + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag))}
```

It produces `<baseUrl>[/<subPath>]/<name>`, where the name is:

- **Local blob** (OCI manifest media type): `access.referenceName` verbatim (whatever it contains, including host/port/digest). E.g. `ghcr.io/org/image:v1` → `<target>/ghcr.io/org/image:v1`. Same as the old `--upload-as ociArtifact`.
- **Helm**: path of `helmRepository` (empty segments dropped) + chart name, tagged with `version` or the part after `:` in `helmChart`. E.g. `https://stefanprodan.github.io/podinfo` + `podinfo:6.5.0` → `podinfo/podinfo:6.5.0`. Same as old.
- **OCI image**: `resource.access.toOCI().repository` + tag (registry and digest dropped). E.g. `ghcr.io/org/image:v1` → `org/image:v1`. Same as old.

Writing it out explicitly is equivalent to omitting it.

The template is evaluated for every selected resource while the graph is built.
A template that does not evaluate for a selected resource fails the transfer
with `imageReference does not evaluate`, for example the default template on a
CTF target (it reads `target.baseUrl`), or `resource.access.toOCI()` on a Helm
chart. A template that does not use `target` (for example an absolute registry
prefix) also works for CTF targets, because `TransferOCIArtifact` and
`AddOCIArtifact` push to the templated image reference independently of the
component target; its `match` must then select CTF targets (see E7 below).

#### More examples

Relocate local blobs under a mirror using `referenceName` directly. The
default `match` also selects OCI images and Helm charts, which have no
`referenceName`, so restrict the uploader to local blobs:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.access.isType("LocalBlob") && isOCIManifest(resource.access.mediaType) && has(resource.access.referenceName)
  imageReference: '${"ghcr.io/mirror/" + resource.access.referenceName}'
```

Build a reference from resource metadata (works for any resource the uploader can upload):

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  imageReference: '${target.baseUrl + "/" + resource.name + ":" + resource.version}'
```

Upload a local blob to its `referenceName` as-is, e.g. when the name already is a
full reference such as `ghcr.io/org/image:v1`:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.name == "my-image"
  imageReference: '${resource.access.referenceName}'
```

See [Migrate from --upload-as to Uploader Configurations]({{< relref "docs/how-to/migrate-from-upload-as.md" >}}).

### `localblob.uploader.transfer.config.ocm.software/v1alpha1`

Downloads a selected resource and embeds it in the target as a local blob. This
is the same operation the baseline applies to local blobs, extended to any
supported access type: OCI images are fetched via `GetOCIArtifact`, Helm charts
converted to OCI via `GetHelmChart` → `ConvertHelmToOCI`, and wget, S3 and
GitHub resources are downloaded through their access-specific downloaders.

Selecting an access type the local blob uploader cannot handle fails the transfer
with `local blob uploader cannot copy access type …`.

#### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/LocalBlobUploaderConfig.schema.json" >}}

#### Fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `match` | CEL expression | no | A CEL boolean expression selecting the resources this uploader handles (`resource` and `target` are available; test access types with `resource.access.isType`). When omitted, the default below applies; an explicit value replaces it. |

#### Default `match`

```yaml
match: resource.access.isType(["LocalBlob", "OCIImage", "Helm", "Wget", "S3", "GitHub"])
```

The default selects the access types the uploader can handle. An explicit `match`
replaces the default entirely.

A plain entry with no fields is equivalent to `copyMode: allResources` in old
configs.

### `reference.uploader.transfer.config.ocm.software/v1alpha1`

Keeps a selected resource by reference: no transformation is applied and the
resource's access is unchanged in the target. This is the same as what the
baseline does for non-local-blob resources, but expressed as an explicit uploader
entry so it can be placed before a catch-all to exclude specific resources from
copying.

Selecting a local blob fails the transfer with
`local blobs cannot be kept by reference`.

#### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/ReferenceUploaderConfig.schema.json" >}}

#### Fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `match` | CEL expression | no | A CEL boolean expression selecting the resources this uploader handles (`resource` and `target` are available; test access types with `resource.access.isType`). When omitted, the default below applies; an explicit value replaces it. |

#### Default `match`

```yaml
match: '!resource.access.isType("LocalBlob")'
```

The default excludes local blobs because selecting a local blob is an error.
An explicit `match` replaces the default entirely.

### Complete Example

The following shows a full config migrated from the old `copyMode: allResources`
plus `uploadType: ociArtifact` style.

Old:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
    copyMode: allResources      # copy everything …
    uploadType: ociArtifact     # … and push OCI content as separate artifacts
```

New:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
  # 1. Keep the large base image by reference (not copied at all).
  #    Declared first, so the catch-all below never sees it.
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "base-os-image"
  # 2. Stream wget-hosted documentation to an HTTP artifact store.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://artifacts.example.com/ocm" + url(resource.access.url).path}'
    method: PUT
  # 3. OCI images, Helm charts and OCI-manifest local blobs become separate OCI
  #    artifacts next to the component version (former uploadType: ociArtifact).
  #    Default match and imageReference.
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  # 4. Everything the rules above did not select is embedded as a local blob
  #    (former copyMode: allResources). Default match.
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

Entries 3 and 4 need no fields: their default `match` (see
[Default `match`](#default-match) and the local blob uploader section) and the
default `imageReference` are what the former settings did.

Outcome for `ocm.software/demo:1.0.0` transferred to `ghcr.io/target-org/ocm`:

| Resource | Access | Selected by | Result in target |
| --- | --- | --- | --- |
| `base-os-image` | `OCIImage/v1` `ghcr.io/acme/base-os:1.2` | 1 | unchanged `ghcr.io/acme/base-os:1.2` |
| `docs` | `Wget/v1` `https://docs.example.com/demo/guide.tar` | 2 | `Wget/v1` `https://artifacts.example.com/ocm/demo/guide.tar` |
| `app-image` | `OCIImage/v1` `ghcr.io/acme/app:1.0.0` | 3 | `ociArtifact/v1` `ghcr.io/target-org/ocm/acme/app:1.0.0` |
| `chart` | `helm/v1` `https://charts.acme.io/stable` + `app:1.0.0` | 3 | `ociArtifact/v1` `ghcr.io/target-org/ocm/stable/app:1.0.0` |
| `config` | `LocalBlob/v1` `application/json` | 4 (3's `match` is false: not an OCI manifest) | `LocalBlob/v1` |
| `sources` | `GitHub/v1` (pinned commit) | 4 | `LocalBlob/v1` |
| `models` | `S3/v2` | 4 | `LocalBlob/v1` |

Transferring the same config to a CTF target: entry 3's `match` is false for every
resource, so `app-image` and `chart` are copied as local blobs by entry 4.

### Selection Examples

The following examples are executed as tests (`TestUploaderExamples` in
`bindings/go/transfer/internal`), so each outcome below is what the transfer
does. They all transfer the component `ocm.software/demo:1.0.0` and, unless
stated otherwise, target the OCI registry `ghcr.io/target-org/ocm`
(`baseUrl: ghcr.io/target-org/ocm`, `subPath: ""`). The component has these
resources:

| Name | Access |
| --- | --- |
| `app` | `ociArtifact/v1` `imageReference: ghcr.io/acme/app:1.0.0`; label `{name: ocm.software/transfer, value: oci}` |
| `nginx` | `ociArtifact/v1` `imageReference: docker.io/library/nginx:1.25` |
| `chart` | `helm/v1` `helmRepository: https://charts.acme.io/stable`, `helmChart: app`, `version: 1.0.0` |
| `bundle` | `LocalBlob/v1` `mediaType: application/vnd.oci.image.manifest.v1+json`, `referenceName: acme/bundle:1.0.0` |
| `notes` | `LocalBlob/v1` `mediaType: text/plain` |
| `docs` | `Wget/v1` `url: https://docs.acme.io/guide.tar` |

Outcomes:

- **oci `<ref>`**: pushed as a separate OCI artifact to `<ref>`.
- **local blob**: embedded in the target as a local blob.
- **by reference**: not copied; the access is unchanged in the target.

The baseline applies to resources no uploader selects: local blobs are copied
as local blobs and all other resources stay by reference. A catch-all
`localblob.uploader…` entry copies every supported resource.

#### E1 — default OCI uploader (replaces `--upload-as ociArtifact`)

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome |
| --- | --- |
| `app` | oci `ghcr.io/target-org/ocm/acme/app:1.0.0` |
| `nginx` | oci `ghcr.io/target-org/ocm/library/nginx:1.25` |
| `chart` | oci `ghcr.io/target-org/ocm/stable/app:1.0.0` |
| `bundle` | oci `ghcr.io/target-org/ocm/acme/bundle:1.0.0` |
| `notes` | local blob (the default `match` does not select it: not an OCI manifest) |
| `docs` | by reference (the default `match` does not select `Wget`) |

#### E2 — the same with every default spelled out

Writing the default `match` and the default
[`imageReference`](#imagereference) into the entry gives the same outcome as
E1. There is no reason to do so except as a starting point for a change.

#### E3 — Helm charts only

An explicit `match` replaces the default entirely, so it must repeat the target check.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: target.type == "OCIRepository" && resource.access.isType("Helm")
```

| Resource | Outcome |
| --- | --- |
| `chart` | oci `ghcr.io/target-org/ocm/stable/app:1.0.0` |
| `app`, `nginx`, `docs` | by reference |
| `bundle`, `notes` | local blob |

#### E4 — OCI-manifest local blobs only (replaces `--upload-as ociArtifact` without a local blob catch-all)

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("LocalBlob")
      && isOCIManifest(resource.access.mediaType)
      && has(resource.access.referenceName)
```

| Resource | Outcome |
| --- | --- |
| `bundle` | oci `ghcr.io/target-org/ocm/acme/bundle:1.0.0` |
| `notes` | local blob |
| `app`, `nginx`, `chart`, `docs` | by reference |

#### E5 — only images from Docker Hub

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("OCIImage")
      && resource.access.toOCI().host.endsWith("docker.io")
```

| Resource | Outcome |
| --- | --- |
| `nginx` | oci `ghcr.io/target-org/ocm/library/nginx:1.25` |
| `app`, `chart`, `docs` | by reference |
| `bundle`, `notes` | local blob |

`toOCI()` normalizes Docker Hub references to the host `registry-1.docker.io`,
hence `endsWith`. `toOCI()` is only called for `OCIImage` accesses: `&&`
short-circuits, so the Helm and local blob resources never reach it.

#### E6 — select by label

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("OCIImage")
      && has(resource.labels)
      && resource.labels.exists(l, l.name == "ocm.software/transfer" && l.value == "oci")
```

| Resource | Outcome |
| --- | --- |
| `app` | oci `ghcr.io/target-org/ocm/acme/app:1.0.0` |
| `nginx`, `chart`, `docs` | by reference |
| `bundle`, `notes` | local blob |

`has(resource.labels)` is required because `labels` is omitted from a resource
that has none.

#### E7 — CTF target, images mirrored to a registry

Target `ctf::./archive`. The template does not use `target`, so it evaluates
for a CTF target.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("OCIImage")
    imageReference: '${"registry.example.com/mirror/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
```

| Resource | Outcome |
| --- | --- |
| `app` | oci `registry.example.com/mirror/acme/app:1.0.0` |
| `nginx` | oci `registry.example.com/mirror/library/nginx:1.25` |
| `chart`, `docs` | by reference |
| `bundle`, `notes` | local blob |

#### E8 — relocate one resource, default for the rest

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "app"
    imageReference: ghcr.io/target-org/special/app:1.0.0
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome |
| --- | --- |
| `app` | oci `ghcr.io/target-org/special/app:1.0.0` (first entry: `match` selects by name, default `match` of second entry would also select but first wins) |
| `nginx`, `chart`, `bundle`, `notes`, `docs` | as in E1 (second entry) |

#### E9 — errors instead of silent skipping

Each case transfers a component with only the named resource.

| Config entry | Resource / target | Error contains |
| --- | --- | --- |
| OCI, `match: resource.access.isType("Wget")` | `docs` | `oci uploader cannot upload access type Wget/v1` |
| OCI, `match: resource.access.isType("LocalBlob")` | `notes` | `not an OCI manifest` |
| OCI, `match: resource.access.isType(` | `app` | `invalid match` |
| OCI, `match: '"yes"'` | `app` | `must evaluate to a bool` |
| OCI, `match: resource.access.isType("OCIImage")`, default `imageReference` | `app`, target `ctf::./archive` | `imageReference does not evaluate` (the default template reads `target.baseUrl`, which a CTF target does not have) |

#### E10 — copy everything as local blobs (replaces `copyMode: allResources`)

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome |
| --- | --- |
| `app` | local blob |
| `nginx` | local blob |
| `chart` | local blob |
| `bundle` | local blob |
| `notes` | local blob |
| `docs` | local blob |

`chart` goes through GetHelmChart → ConvertHelmToOCI → OCIAddLocalResource;
`docs` through DownloadWgetResource → OCIAddLocalResource.
A config with only a `localblob.uploader…` entry (no other uploaders) produces the same graph.

#### E11 — OCI artifacts plus everything else copied (replaces `copyMode: allResources` + `uploadType: ociArtifact`)

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome |
| --- | --- |
| `app` | oci `ghcr.io/target-org/ocm/acme/app:1.0.0` |
| `nginx` | oci `ghcr.io/target-org/ocm/library/nginx:1.25` |
| `chart` | oci `ghcr.io/target-org/ocm/stable/app:1.0.0` |
| `bundle` | oci `ghcr.io/target-org/ocm/acme/bundle:1.0.0` |
| `notes` | local blob |
| `docs` | local blob |

#### E12 — exclude one resource from a catch-all

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "nginx"
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome |
| --- | --- |
| `nginx` | by reference |
| `app` | oci `ghcr.io/target-org/ocm/acme/app:1.0.0` |
| `chart` | oci `ghcr.io/target-org/ocm/stable/app:1.0.0` |
| `bundle` | oci `ghcr.io/target-org/ocm/acme/bundle:1.0.0` |
| `notes` | local blob |
| `docs` | local blob |

#### E13 — keep Docker Hub images by reference, copy the rest

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("OCIImage") && resource.access.toOCI().host.endsWith("docker.io")
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome |
| --- | --- |
| `nginx` | by reference |
| `app` | local blob |
| `chart` | local blob |
| `bundle` | local blob |
| `notes` | local blob |
| `docs` | local blob |

#### E14 — copy only wget downloads

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget")
```

| Resource | Outcome |
| --- | --- |
| `docs` | local blob |
| `app`, `nginx`, `chart` | by reference |
| `bundle`, `notes` | local blob (baseline) |

#### E15 — errors

Each case transfers a component with only the named resource.

| Config entry | Resource | Error contains |
| --- | --- | --- |
| reference, `match: "true"` | `bundle` | `local blobs cannot be kept by reference` |
| localblob, `match: "true"` | `custom` with access `{"type": "Custom/v1"}` | `local blob uploader cannot copy access type Custom/v1` |

The [Complete Example](#complete-example) above is executed as E16.

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
| `match`               | CEL expression        | —                                 | Selects the resources this uploader handles. Required: no default for HTTP uploaders.  |
| `targetURL`           | CEL expression        | request + published (`url`)       | The upload URL; also the published download URL. See CEL Expressions below.            |
| `method`              | string                | request (`verb`)                  | HTTP method for the upload request. Defaults to PUT. Not on the published access.      |
| `header`              | `map[string][]string` | request                           | HTTP headers sent with the upload request. May be CEL-templated. Request only.         |
| `noRedirect`          | bool                  | request                           | Disable following HTTP redirects on the upload. Not on the published access.           |
| `mediaType`           | string                | request + published (`mediaType`) | Media type recorded on the resource. Defaults to the source's.                         |

### Routing Resources to Different Targets

Because a rule can match on any resource property, several resources of
the **same** access type can be routed to **different** targets. List the specific
rules first; a broader rule acts as a catch-all:

```yaml
configurations:
  # Docs go to the docs bucket.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1") && resource.name == "docs"
    targetURL: '${"https://docs.example.com" + url(resource.access.url).path}'
  # Everything else Wget goes to the generic bucket.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
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
the uploader matches — scope the rule with `match` so all matched
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
    match: resource.access.isType("Wget/v1")
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
    match: resource.access.isType("Wget/v1")
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
    match: resource.access.isType("Wget/v1")
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
whose `match` selects the resource handles it. A
selected uploader that cannot handle the resource fails the transfer. A resource
no uploader selects follows the baseline: local blobs are copied as local blobs,
all other resources stay by reference. See [Selection](#selection).

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
