package tar

import (
	"bytes"
	"fmt"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// The verifier and the reader must agree on which manifests index.json lists:
// a layout the verifier accepts for root must make the reader serve root.
func TestReviewPR4206_S1_IndexCaseVariantServesUnverifiedRoot(t *testing.T) {
	r := require.New(t)

	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageConfig, configData)
	descJSON := func(d ociImageSpecV1.Descriptor) string {
		return fmt.Sprintf(`{"mediaType":%q,"digest":%q,"size":%d}`, d.MediaType, d.Digest, d.Size)
	}
	manifestWith := func(layerData string) (ociImageSpecV1.Descriptor, []byte, ociImageSpecV1.Descriptor, []byte) {
		layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, []byte(layerData))
		data := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":%q,"config":%s,"layers":[%s]}`,
			ociImageSpecV1.MediaTypeImageManifest, descJSON(config), descJSON(layer)))
		return content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, data), data, layer, []byte(layerData)
	}

	good, goodData, goodLayer, goodLayerData := manifestWith("good")
	evil, evilData, evilLayer, evilLayerData := manifestWith("evil")

	// "manifests" and "Manifests" are distinct members to a case-sensitive decoder; for
	// encoding/json v1 both name the Manifests field and the later one wins.
	indexData := []byte(fmt.Sprintf(`{"schemaVersion":2,"manifests":[%s],"Manifests":[%s]}`, descJSON(evil), descJSON(good)))
	archive := gzippedTar(t,
		tarEntry{name: ociImageSpecV1.ImageLayoutFile, data: []byte(`{"imageLayoutVersion":"1.0.0"}`)},
		tarEntry{name: ociImageSpecV1.ImageIndexFile, data: indexData},
		blobEntry(config, configData),
		blobEntry(good, goodData), blobEntry(goodLayer, goodLayerData),
		blobEntry(evil, evilData), blobEntry(evilLayer, evilLayerData),
	)

	r.NoError(verifyLayout(t.Context(), archive, good.Digest), "precondition: the verifier accepts the layout for the good root")

	store, err := ReadOCILayout(t.Context(), inmemory.New(bytes.NewReader(archive)))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(store.Close()) })

	main := store.MainArtifacts(t.Context())
	r.Len(main, 1)
	r.Equal(good.Digest, main[0].Digest, "reader serves %s, a root the verifier never checked", main[0].Digest)
}
