package sync

import (
	"cmp"
	"context"
)

type GraphDiscovererOptions[K cmp.Ordered, V any] struct {
	// Algorithm is the discovery strategy. Required — returns error if nil.
	Algorithm DiscoveryAlgorithm[K, V]

	// Roots is the set of starting vertex keys.
	Roots []K

	// Resolver maps a vertex key to its resolved value (typically external I/O).
	// Must be concurrency-safe.
	Resolver Resolver[K, V]

	// Discoverer maps a resolved value to its child vertex keys.
	// Must be concurrency-safe.
	Discoverer Discoverer[K, V]
}

type Resolver[K cmp.Ordered, V any] interface {
	Resolve(ctx context.Context, key K) (value V, err error)
}

type ResolverFunc[K cmp.Ordered, V any] func(ctx context.Context, key K) (value V, err error)

func (f ResolverFunc[K, V]) Resolve(ctx context.Context, key K) (value V, err error) {
	return f(ctx, key)
}

type Discoverer[K cmp.Ordered, V any] interface {
	Discover(ctx context.Context, parent V) (children []K, err error)
}

type DiscovererFunc[K cmp.Ordered, V any] func(ctx context.Context, parent V) (children []K, err error)

func (f DiscovererFunc[K, V]) Discover(ctx context.Context, parent V) (children []K, err error) {
	return f(ctx, parent)
}
