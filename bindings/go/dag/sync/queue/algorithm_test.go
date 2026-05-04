package queue_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/dag/sync/queue"
)

func newDiscoverer[V any](workers int, roots []string, resolver dagsync.ResolverFunc[string, V], discoverer dagsync.DiscovererFunc[string, V]) *dagsync.GraphDiscoverer[string, V] {
	return dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, V]{
		Algorithm:  &queue.Algorithm[string, V]{Workers: workers},
		Roots:      roots,
		Resolver:   resolver,
		Discoverer: discoverer,
	})
}

func TestQueueAlgorithm(t *testing.T) {
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
			4,
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
			1,
			[]string{"A"},
			dagsync.ResolverFunc[string, string](func(_ context.Context, _ string) (string, error) {
				return "", fmt.Errorf("should never reach here")
			}),
			nil,
		)
		r.Error(d.Discover(ctx))
	})

	t.Run("missing node in external graph", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		graph := map[string][]string{
			"A": {"B", "C"},
			"C": {"D"},
		}
		d := newDiscoverer(
			4,
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
	})

	t.Run("single node no children", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		graph := map[string][]string{"B": {}}
		d := newDiscoverer(
			1,
			[]string{"B"},
			dagsync.ResolverFunc[string, string](func(_ context.Context, key string) (string, error) {
				return key, nil
			}),
			dagsync.DiscovererFunc[string, string](func(_ context.Context, parent string) ([]string, error) {
				return graph[parent], nil
			}),
		)
		r.NoError(d.Discover(ctx))
		r.Equal(dagsync.DiscoveryStateCompleted, d.CurrentState("B"))
	})

	// This test verifies the core advantage of the queue algorithm: a concurrency
	// limit of 1 on a deep graph does NOT deadlock, unlike the spawn algorithm.
	t.Run("deep graph with workers=1 does not deadlock", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		// Build a chain: A→B→C→D→E (depth 5)
		graph := map[string][]string{
			"A": {"B"},
			"B": {"C"},
			"C": {"D"},
			"D": {"E"},
			"E": {},
		}
		d := newDiscoverer(
			1, // single worker — would deadlock the spawn algorithm
			[]string{"A"},
			dagsync.ResolverFunc[string, string](func(_ context.Context, key string) (string, error) {
				return key, nil
			}),
			dagsync.DiscovererFunc[string, string](func(_ context.Context, parent string) ([]string, error) {
				return graph[parent], nil
			}),
		)
		r.NoError(d.Discover(ctx))
		r.Equal(dagsync.DiscoveryStateCompleted, d.CurrentState("E"))
	})

	t.Run("concurrency limit is respected", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		const limit = 2
		var active atomic.Int32
		var maxSeen atomic.Int32

		graph := map[string][]string{
			"root": {"A", "B", "C", "D", "E"},
			"A": {}, "B": {}, "C": {}, "D": {}, "E": {},
		}
		d := newDiscoverer(
			limit,
			[]string{"root"},
			dagsync.ResolverFunc[string, string](func(_ context.Context, key string) (string, error) {
				cur := active.Add(1)
				defer active.Add(-1)
				for {
					old := maxSeen.Load()
					if cur <= old || maxSeen.CompareAndSwap(old, cur) {
						break
					}
				}
				return key, nil
			}),
			dagsync.DiscovererFunc[string, string](func(_ context.Context, parent string) ([]string, error) {
				return graph[parent], nil
			}),
		)
		r.NoError(d.Discover(ctx))
		r.LessOrEqual(maxSeen.Load(), int32(limit), "concurrency exceeded limit")
	})
}
