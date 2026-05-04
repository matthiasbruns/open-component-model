package spawn_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/dag/sync/spawn"
)

func newDiscoverer[V any](roots []string, resolver dagsync.ResolverFunc[string, V], discoverer dagsync.DiscovererFunc[string, V]) *dagsync.GraphDiscoverer[string, V] {
	return dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, V]{
		Algorithm:  &spawn.Algorithm[string, V]{},
		Roots:      roots,
		Resolver:   resolver,
		Discoverer: discoverer,
	})
}

func TestSpawnAlgorithm(t *testing.T) {
	t.Run("graph discovery succeeds", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		graph := map[string][]string{
			"A": {"B", "C"},
			"B": {"D"},
			"C": {"D"},
			"D": {},
		}
		d := newDiscoverer(
			[]string{"A", "B", "C", "D"},
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
		r.NoError(d.Discover(ctx))
		r.ElementsMatch(d.CurrentEdges("A"), []string{"B", "C"})
		r.ElementsMatch(d.CurrentEdges("B"), []string{"D"})
		r.ElementsMatch(d.CurrentEdges("C"), []string{"D"})
		r.ElementsMatch(d.CurrentEdges("D"), []string{})
	})

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		r := require.New(t)
		d := newDiscoverer(
			[]string{"A"},
			dagsync.ResolverFunc[string, string](func(_ context.Context, _ string) (string, error) {
				return "", fmt.Errorf("should never reach here")
			}),
			nil,
		)
		r.ErrorIs(d.Discover(ctx), context.Canceled)
	})

	t.Run("missing node in external graph", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		graph := map[string][]string{
			"A": {"B", "C"},
			"C": {"D"},
		}
		d := newDiscoverer(
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
		r.Error(d.Discover(ctx))
		r.Equal(dagsync.DiscoveryStateError, d.CurrentState("A"))
	})

	t.Run("single node no children", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		graph := map[string][]string{"B": {}}
		d := newDiscoverer(
			[]string{"B"},
			dagsync.ResolverFunc[string, string](func(_ context.Context, key string) (string, error) {
				if _, ok := graph[key]; !ok {
					return "", fmt.Errorf("no node found with ID %s", key)
				}
				return key, nil
			}),
			dagsync.DiscovererFunc[string, string](func(_ context.Context, parent string) ([]string, error) {
				return graph[parent], nil
			}),
		)
		r.NoError(d.Discover(ctx))
		r.Equal(dagsync.DiscoveryStateCompleted, d.CurrentState("B"))
	})
}
