package sync_test

import (
	"cmp"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/dag/sync/queue"
	"ocm.software/open-component-model/bindings/go/dag/sync/spawn"
)

type algorithmCase[K cmp.Ordered, V any] struct {
	name      string
	algorithm dagsync.DiscoveryAlgorithm[K, V]
}

func allAlgorithms[V any]() []algorithmCase[string, V] {
	return []algorithmCase[string, V]{
		{name: "spawn", algorithm: &spawn.Algorithm[string, V]{}},
		{name: "queue/workers=1", algorithm: &queue.Algorithm[string, V]{Workers: 1}},
		{name: "queue/workers=4", algorithm: &queue.Algorithm[string, V]{Workers: 4}},
		{name: "queue/default", algorithm: &queue.Algorithm[string, V]{}},
	}
}

func makeDiscoverer[V any](alg dagsync.DiscoveryAlgorithm[string, V], roots []string, res dagsync.ResolverFunc[string, V], dis dagsync.DiscovererFunc[string, V]) *dagsync.GraphDiscoverer[string, V] {
	return dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, V]{
		Algorithm:  alg,
		Roots:      roots,
		Resolver:   res,
		Discoverer: dis,
	})
}

func TestDiscoveryAlgorithms_Diamond(t *testing.T) {
	graph := map[string][]string{
		"A": {"B", "C"},
		"B": {"D"},
		"C": {"D"},
		"D": {},
	}
	for _, tc := range allAlgorithms[string]() {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			d := makeDiscoverer(
				tc.algorithm,
				[]string{"A"},
				dagsync.ResolverFunc[string, string](func(_ context.Context, key string) (string, error) {
					if _, ok := graph[key]; !ok {
						return "", fmt.Errorf("unknown node %s", key)
					}
					return key, nil
				}),
				dagsync.DiscovererFunc[string, string](func(_ context.Context, parent string) ([]string, error) {
					return graph[parent], nil
				}),
			)
			r.NoError(d.Discover(t.Context()))
			r.ElementsMatch(d.CurrentEdges("A"), []string{"B", "C"})
			r.ElementsMatch(d.CurrentEdges("B"), []string{"D"})
			r.ElementsMatch(d.CurrentEdges("C"), []string{"D"})
			r.Empty(d.CurrentEdges("D"))
			r.Equal(dagsync.DiscoveryStateCompleted, d.CurrentState("A"))
			r.Equal(dagsync.DiscoveryStateCompleted, d.CurrentState("D"))
		})
	}
}

func TestDiscoveryAlgorithms_MultiRoot(t *testing.T) {
	graph := map[string][]string{
		"A": {"C"},
		"B": {"C"},
		"C": {},
	}
	for _, tc := range allAlgorithms[string]() {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			d := makeDiscoverer(
				tc.algorithm,
				[]string{"A", "B"},
				dagsync.ResolverFunc[string, string](func(_ context.Context, key string) (string, error) {
					return key, nil
				}),
				dagsync.DiscovererFunc[string, string](func(_ context.Context, parent string) ([]string, error) {
					return graph[parent], nil
				}),
			)
			r.NoError(d.Discover(t.Context()))
			r.ElementsMatch(d.CurrentEdges("A"), []string{"C"})
			r.ElementsMatch(d.CurrentEdges("B"), []string{"C"})
			r.Empty(d.CurrentEdges("C"))
		})
	}
}

func TestDiscoveryAlgorithms_ContextCanceled(t *testing.T) {
	for _, tc := range allAlgorithms[string]() {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			d := makeDiscoverer(
				tc.algorithm,
				[]string{"A"},
				dagsync.ResolverFunc[string, string](func(_ context.Context, _ string) (string, error) {
					return "", fmt.Errorf("should not be called")
				}),
				nil,
			)
			r.Error(d.Discover(ctx))
		})
	}
}

func TestDiscoveryAlgorithms_ResolverError(t *testing.T) {
	graph := map[string][]string{
		"A": {"B", "C"},
		"C": {"D"},
	}
	for _, tc := range allAlgorithms[string]() {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			d := makeDiscoverer(
				tc.algorithm,
				[]string{"A"},
				dagsync.ResolverFunc[string, string](func(_ context.Context, key string) (string, error) {
					if _, ok := graph[key]; !ok {
						return "", fmt.Errorf("no node found with ID %s", key)
					}
					return key, nil
				}),
				dagsync.DiscovererFunc[string, string](func(_ context.Context, parent string) ([]string, error) {
					dep, ok := graph[parent]
					if !ok {
						return nil, fmt.Errorf("no node found with ID %s", parent)
					}
					return dep, nil
				}),
			)
			r.Error(d.Discover(t.Context()))
		})
	}
}

func TestDiscovery_NoAlgorithm(t *testing.T) {
	r := require.New(t)
	d := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, string]{
		Roots:    []string{"A"},
		Resolver: dagsync.ResolverFunc[string, string](func(_ context.Context, key string) (string, error) { return key, nil }),
	})
	r.Error(d.Discover(t.Context()))
}

func TestDiscovery_NoRoots(t *testing.T) {
	r := require.New(t)
	d := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, string]{
		Algorithm: &spawn.Algorithm[string, string]{},
	})
	r.Error(d.Discover(t.Context()))
}
