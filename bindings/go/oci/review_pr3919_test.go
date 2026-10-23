package oci_test

import (
	stdtar "archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Review evidence for PR #3919. Each test asserts the behaviour the PR claims
// ("content swapped under its recorded digest ... is rejected before bytes are
// served") and fails on the PR head where the v1 artifact-set fallback accepts
// content that the recorded digest does not cover.

const reviewPR3919Root = "62be4af3382a4493cb7f1dd4ec47bcb28f1863b615fc9e4a1dceefbe93898dd0"

type reviewPR3919Entry struct {
	name string
	data []byte
}

// reviewPR3919V1Fixture returns the entries of the real OCM v1 artifact set the PR's own
// tests use (index.json, oci-layout, blobs/sha256.<hex>).
func reviewPR3919V1Fixture(t *testing.T) []reviewPR3919Entry {
	t.Helper()
	r := require.New(t)
	raw, err := os.ReadFile(filepath.Join("..", "ctf", "testdata", "compatibility", "02", "with-resource",
		"blobs", "sha256.e40e3a2f1ab1a98328dfd14539a79d27aff5c4d5c34cd16a85f0288bfa76490b"))
	r.NoError(err)
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	r.NoError(err)
	tr := stdtar.NewReader(gz)
	var entries []reviewPR3919Entry
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)
		if hdr.Typeflag != stdtar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		r.NoError(err)
		entries = append(entries, reviewPR3919Entry{name: hdr.Name, data: data})
	}
	return entries
}

func reviewPR3919Archive(t *testing.T, entries []reviewPR3919Entry, gzipped bool) []byte {
	t.Helper()
	r := require.New(t)
	var buf bytes.Buffer
	var w io.Writer = &buf
	var gz *gzip.Writer
	if gzipped {
		gz = gzip.NewWriter(&buf)
		w = gz
	}
	tw := stdtar.NewWriter(w)
	for _, e := range entries {
		r.NoError(tw.WriteHeader(&stdtar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.data)), Typeflag: stdtar.TypeReg}))
		_, err := tw.Write(e.data)
		r.NoError(err)
	}
	r.NoError(tw.Close())
	if gz != nil {
		r.NoError(gz.Close())
	}
	return buf.Bytes()
}

func reviewPR3919Find(t *testing.T, entries []reviewPR3919Entry, name string) []byte {
	t.Helper()
	for _, e := range entries {
		if e.name == name {
			return e.data
		}
	}
	t.Fatalf("fixture entry %s not found", name)
	return nil
}

// reviewPR3919DecoyMain keeps the signed manifest M as an orphan blob but declares a
// different manifest M' as the artifact set's main artifact (index entry and
// software.ocm/main annotation). OCM v1's ociArtifactDigest digester and every reader
// in this repo resolve the declared main, so consumers use M', which nobody signed.
func reviewPR3919DecoyMain(t *testing.T) ([]byte, digest.Digest) {
	t.Helper()
	r := require.New(t)
	entries := reviewPR3919V1Fixture(t)
	signed := reviewPR3919Find(t, entries, "blobs/sha256."+reviewPR3919Root)

	var manifest map[string]any
	r.NoError(json.Unmarshal(signed, &manifest))
	manifest["annotations"].(map[string]any)["org.opencontainers.image.description"] = "attacker-chosen main artifact"
	decoy, err := json.Marshal(manifest)
	r.NoError(err)
	decoyDigest := digest.FromBytes(decoy)

	index := ociImageSpecV1.Index{
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{{
			MediaType:   ociImageSpecV1.MediaTypeImageManifest,
			Digest:      decoyDigest,
			Size:        int64(len(decoy)),
			Annotations: map[string]string{ociImageSpecV1.AnnotationRefName: "6.7.1", "software.ocm/tags": "6.7.1"},
		}},
		Annotations: map[string]string{"software.ocm/main": decoyDigest.String()},
	}
	index.SchemaVersion = 2
	indexRaw, err := json.Marshal(index)
	r.NoError(err)

	var out []reviewPR3919Entry
	for _, e := range entries {
		if e.name == "index.json" {
			e.data = indexRaw
		}
		out = append(out, e)
	}
	out = append(out, reviewPR3919Entry{name: "blobs/sha256." + decoyDigest.Encoded(), data: decoy})
	return reviewPR3919Archive(t, out, true), decoyDigest
}

// reviewPR3919SwappedLayer keeps index and manifest authentic but replaces the helm
// chart layer bytes under their original file name.
func reviewPR3919SwappedLayer(t *testing.T) []byte {
	t.Helper()
	entries := reviewPR3919V1Fixture(t)
	for i := range entries {
		if strings.HasPrefix(entries[i].name, "blobs/sha256.fe377feb") {
			entries[i].data = []byte("attacker-chosen chart content")
		}
	}
	return reviewPR3919Archive(t, entries, true)
}

func reviewPR3919V1Resource(d *descriptor.Digest) *descriptor.Resource {
	return &descriptor.Resource{
		Relation:    descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "repro", Version: "1.0.0"}},
		Type:        "test-type",
		Access:      &v2.LocalBlob{MediaType: "application/vnd.oci.image.manifest.v1+tar+gzip"},
		Digest:      d,
	}
}

func reviewPR3919Signed(value string, normalisation string) *descriptor.Digest {
	return &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: normalisation, Value: value}
}

// F1: the declared main artifact of a v1 artifact set must be the one the recorded
// digest names; carrying the signed manifest somewhere in the archive is not enough.
func TestReviewPR3919_F1_ArtifactSetDecoyMainArtifact(t *testing.T) {
	archive, decoy := reviewPR3919DecoyMain(t)

	t.Run("consumers resolve the decoy, not the signed root", func(t *testing.T) {
		r := require.New(t)
		as, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive),
			inmemory.WithMediaType("application/vnd.oci.image.manifest.v1+tar+gzip")))
		r.NoError(err)
		t.Cleanup(func() { _ = as.Close() })
		idx := as.GetIndex()
		r.Equal(decoy.String(), idx.Annotations["software.ocm/main"])
		r.Len(idx.Manifests, 1)
		r.Equal(decoy, idx.Manifests[0].Digest)
		r.NotEqual(reviewPR3919Root, decoy.Encoded())
	})

	t.Run("AddLocalResource rejects", func(t *testing.T) {
		r := require.New(t)
		_, err := newCTFRepository(t).AddLocalResource(t.Context(), "ocm.software/repro", "1.0.0",
			reviewPR3919V1Resource(reviewPR3919Signed(reviewPR3919Root, "ociArtifactDigest/v1")),
			inmemory.New(bytes.NewReader(archive)))
		r.Error(err, "an artifact set whose declared main artifact is not the signed root must be rejected")
	})

	t.Run("GetLocalResource rejects", func(t *testing.T) {
		r := require.New(t)
		repo := newCTFRepository(t)
		addAndCommit(t, repo, reviewPR3919V1Resource(nil), archive, func(d *descriptor.Digest) {
			*d = *reviewPR3919Signed(reviewPR3919Root, "ociArtifactDigest/v1")
		})
		_, _, err := repo.GetLocalResource(t.Context(), "ocm.software/repro", "1.0.0",
			map[string]string{"name": "repro", "version": "1.0.0"})
		r.Error(err, "an artifact set whose declared main artifact is not the signed root must not be served")
	})
}

// F3: the fallback hashes only the root manifest blob; the layers that manifest
// references are served unverified.
func TestReviewPR3919_F3_ArtifactSetSwappedLayer(t *testing.T) {
	archive := reviewPR3919SwappedLayer(t)

	t.Run("AddLocalResource rejects", func(t *testing.T) {
		r := require.New(t)
		_, err := newCTFRepository(t).AddLocalResource(t.Context(), "ocm.software/repro", "1.0.0",
			reviewPR3919V1Resource(reviewPR3919Signed(reviewPR3919Root, "ociArtifactDigest/v1")),
			inmemory.New(bytes.NewReader(archive)))
		r.Error(err, "an artifact set whose layer bytes do not match the signed manifest must be rejected")
	})

	t.Run("GetLocalResource rejects", func(t *testing.T) {
		r := require.New(t)
		repo := newCTFRepository(t)
		addAndCommit(t, repo, reviewPR3919V1Resource(nil), archive, func(d *descriptor.Digest) {
			*d = *reviewPR3919Signed(reviewPR3919Root, "ociArtifactDigest/v1")
		})
		_, _, err := repo.GetLocalResource(t.Context(), "ocm.software/repro", "1.0.0",
			map[string]string{"name": "repro", "version": "1.0.0"})
		r.Error(err, "an artifact set whose layer bytes do not match the signed manifest must not be served")
	})
}

// F2: the v1 artifact-set fallback is not limited to ociArtifactDigest/v1 or to artifact
// set media types. A plain blob signed with genericBlobDigest/v1 can be replaced by any
// tar that carries the signed bytes at blobs/sha256.<hex> next to attacker content, which
// is the reported swap attack with one extra wrapping step.
func TestReviewPR3919_F2_GenericBlobWrappedInTar(t *testing.T) {
	signedContent := []byte("the signed content")
	signedDigest := digest.FromBytes(signedContent)
	wrapper := reviewPR3919Archive(t, []reviewPR3919Entry{
		{name: "blobs/sha256." + signedDigest.Encoded(), data: signedContent},
		{name: "payload.sh", data: []byte("#!/bin/sh\necho attacker\n")},
	}, false)
	signed := reviewPR3919Signed(signedDigest.Encoded(), "genericBlobDigest/v1")

	t.Run("AddLocalResource rejects", func(t *testing.T) {
		r := require.New(t)
		res := newLocalBlobResource("text/plain")
		res.Digest = signed
		_, err := newCTFRepository(t).AddLocalResource(t.Context(), "ocm.software/repro", "1.0.0", res,
			inmemory.New(bytes.NewReader(wrapper)))
		r.Error(err, "a blob that does not hash to its genericBlobDigest must be rejected")
	})

	t.Run("GetLocalResource rejects", func(t *testing.T) {
		r := require.New(t)
		repo := newCTFRepository(t)
		addAndCommit(t, repo, newLocalBlobResource("text/plain"), wrapper, func(d *descriptor.Digest) { *d = *signed })
		b, _, err := repo.GetLocalResource(t.Context(), "ocm.software/repro", "1.0.0",
			map[string]string{"name": "repro", "version": "1.0.0"})
		if err == nil {
			served, rerr := io.ReadAll(mustReader(t, b))
			r.NoError(rerr)
			r.Failf("served content does not match the signed digest",
				"signed %s, served %s (%d bytes, a tar wrapper)", signedDigest, digest.FromBytes(served), len(served))
		}
	})

	t.Run("DownloadResource rejects", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
		r.NoError(err)
		store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
		repo := Repository(t, ocictf.WithCTF(store))

		wrapperDigest := digest.FromBytes(wrapper)
		layerStore, err := store.StoreForReference(ctx, "test-registry/wrapped")
		r.NoError(err)
		r.NoError(layerStore.Push(ctx, ociImageSpecV1.Descriptor{
			MediaType: ociImageSpecV1.MediaTypeImageLayer, Digest: wrapperDigest, Size: int64(len(wrapper)),
		}, bytes.NewReader(wrapper)))

		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "repro", Version: "1.0.0"}},
			Type:        "test-type",
			Access: &v1.OCIImageLayer{
				Type:      runtime.NewVersionedType(v1.OCIImageLayerType, v1.Version),
				Reference: "test-registry/wrapped@" + wrapperDigest.String(),
				MediaType: ociImageSpecV1.MediaTypeImageLayer,
				Digest:    wrapperDigest,
				Size:      int64(len(wrapper)),
			},
			Digest: signed,
		}
		_, err = repo.DownloadResource(ctx, res)
		r.Error(err, "content that does not hash to the signed genericBlobDigest must not be served by DownloadResource")
	})
}
