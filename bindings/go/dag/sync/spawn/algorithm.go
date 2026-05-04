package spawn

import (
	"cmp"
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/dag"
	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
)

// Algorithm implements the recursive spawn-based concurrent discovery strategy.
//
// Each vertex spawns one goroutine per child using errgroup. Concurrency is
// unbounded — do not use errgroup.SetLimit or equivalent, as parent goroutines
// block on Wait() while holding a slot, deadlocking when children need the
// same slots (see https://github.com/open-component-model/ocm-project/issues/735).
//
// Use the queue.Algorithm when bounded concurrency is required.
type Algorithm[K cmp.Ordered, V any] struct{}

var _ dagsync.DiscoveryAlgorithm[string, any] = (*Algorithm[string, any])(nil)

func (a *Algorithm[K, V]) Discover(
	ctx context.Context,
	roots []K,
	graph *dagsync.SyncedDirectedAcyclicGraph[K],
	resolver dagsync.Resolver[K, V],
	discoverer dagsync.Discoverer[K, V],
) error {
	doneMap := &sync.Map{}
	errGroup, egctx := errgroup.WithContext(ctx)
	for _, root := range roots {
		errGroup.Go(func() error {
			return discover(egctx, root, graph, resolver, discoverer, doneMap)
		})
	}
	return errGroup.Wait()
}

func discover[K cmp.Ordered, V any](
	ctx context.Context,
	id K,
	graph *dagsync.SyncedDirectedAcyclicGraph[K],
	resolver dagsync.Resolver[K, V],
	discoverer dagsync.Discoverer[K, V],
	doneMap *sync.Map,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	ch := make(chan struct{})
	defer close(ch)

	doneCh, loaded := doneMap.LoadOrStore(id, ch)
	done := doneCh.(chan struct{})
	if loaded {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		}
	}

	if err := graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		return d.AddVertex(id, map[string]any{
			dagsync.AttributeDiscoveryState: dagsync.DiscoveryStateDiscovering,
		})
	}); err != nil {
		return err
	}

	value, err := resolver.Resolve(ctx, id)

	if stateErr := graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		if err != nil {
			d.Vertices[id].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateError
		} else {
			d.Vertices[id].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateDiscovered
			d.Vertices[id].Attributes[dagsync.AttributeValue] = value
		}
		return err
	}); stateErr != nil {
		return stateErr
	}

	neighbors, err := discoverer.Discover(ctx, value)

	if stateErr := graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		if err != nil {
			d.Vertices[id].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateError
		} else {
			d.Vertices[id].Attributes[dagsync.AttributeValue] = value
		}
		return err
	}); stateErr != nil {
		return stateErr
	}

	errGroup, egctx := errgroup.WithContext(ctx)
	for index, neighbor := range neighbors {
		errGroup.Go(func() error {
			if err := discover(egctx, neighbor, graph, resolver, discoverer, doneMap); err != nil {
				return fmt.Errorf("failed to discover reference %v: %w", neighbor, err)
			}
			return graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
				return d.AddEdge(id, neighbor, map[string]any{
					dagsync.AttributeOrderIndex: index,
				})
			})
		})
	}
	err = errGroup.Wait()

	return graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		if err != nil {
			d.Vertices[id].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateError
		} else {
			d.Vertices[id].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateCompleted
		}
		return err
	})
}
