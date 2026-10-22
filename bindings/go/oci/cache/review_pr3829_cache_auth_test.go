package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"

	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

type reviewStorage struct {
	content.ReadOnlyStorage
	fetch  func(context.Context, ociImageSpecV1.Descriptor) (io.ReadCloser, error)
	exists func(context.Context, ociImageSpecV1.Descriptor) (bool, error)
}

func (s *reviewStorage) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return s.fetch(ctx, target)
}

func (s *reviewStorage) Exists(ctx context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	return s.exists(ctx, target)
}

func TestReviewPR3829_CacheMissChecksEachCaller(t *testing.T) {
	r := require.New(t)
	data := []byte(`{"schemaVersion":2}`)
	target := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	caches := NewCaches(t.TempDir(), &Options{RemotePolicy: RemotePolicyAlways}, nil)
	t.Cleanup(caches.Close)
	cache := caches.Blob()
	r.NotNil(cache)

	started := make(chan struct{})
	release := make(chan struct{})
	allowed := &reviewStorage{
		fetch: func(context.Context, ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
			close(started)
			<-release
			return io.NopCloser(bytes.NewReader(data)), nil
		},
		exists: func(context.Context, ociImageSpecV1.Descriptor) (bool, error) { return true, nil },
	}
	denied := &reviewStorage{
		fetch: func(context.Context, ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
			return nil, errors.New("access denied")
		},
		exists: func(context.Context, ociImageSpecV1.Descriptor) (bool, error) { return false, nil },
	}

	allowedResult := make(chan error, 1)
	go func() {
		reader, err := cache.Fetch(t.Context(), allowed, target)
		if err == nil {
			_, err = io.ReadAll(reader)
			_ = reader.Close()
		}
		allowedResult <- err
	}()
	<-started

	deniedResult := make(chan error, 1)
	go func() {
		reader, err := cache.Fetch(t.Context(), denied, target)
		if err == nil {
			_ = reader.Close()
		}
		deniedResult <- err
	}()
	// Keep the authorized miss in flight while the second caller reaches singleflight.
	time.Sleep(100 * time.Millisecond)
	close(release)

	r.NoError(<-allowedResult)
	r.ErrorContains(<-deniedResult, "access denied")
}

func TestReviewPR3829_ReferenceResolveChecksEachCaller(t *testing.T) {
	r := require.New(t)
	caches := NewCaches(t.TempDir(), nil, &Options{RemotePolicy: RemotePolicyAlways})
	t.Cleanup(caches.Close)
	identity := &identityv1.OCIRegistryIdentity{Hostname: "registry.example.com", Path: "org/repo"}
	cache := caches.Reference(identity)
	r.NotNil(cache)
	ref, err := registry.ParseReference("registry.example.com/org/repo:v1")
	r.NoError(err)
	desc := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Digest: digest.FromBytes([]byte("manifest"))}

	started := make(chan struct{})
	release := make(chan struct{})
	allowed := resolverFn(func(context.Context, string) (ociImageSpecV1.Descriptor, error) {
		close(started)
		<-release
		return desc, nil
	})
	denied := resolverFn(func(context.Context, string) (ociImageSpecV1.Descriptor, error) {
		return ociImageSpecV1.Descriptor{}, errors.New("access denied")
	})

	allowedResult := make(chan error, 1)
	go func() {
		_, err := cache.Resolve(t.Context(), allowed, ref)
		allowedResult <- err
	}()
	<-started

	deniedResult := make(chan error, 1)
	go func() {
		_, err := cache.Resolve(t.Context(), denied, ref)
		deniedResult <- err
	}()
	time.Sleep(100 * time.Millisecond)
	close(release)

	r.NoError(<-allowedResult)
	r.ErrorContains(<-deniedResult, "access denied")
}
