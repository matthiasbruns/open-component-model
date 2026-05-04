package queue

import (
	"cmp"
	"context"
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sync/semaphore"

	"ocm.software/open-component-model/bindings/go/dag"
	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
)

// Algorithm implements bounded-concurrency queue-based graph discovery.
//
// Unlike the spawn algorithm, goroutines do not wait for their children.
// Each goroutine resolves one vertex, enqueues children, then returns.
// A semaphore limits how many vertices are actively resolved at once.
// This prevents the deadlock that occurs in the spawn model when a
// concurrency limit is applied.
//
// Workers defaults to runtime.GOMAXPROCS(0) when <= 0.
type Algorithm[K cmp.Ordered, V any] struct {
	// Workers is the maximum number of vertices resolved concurrently.
	// Defaults to runtime.GOMAXPROCS(0) when <= 0.
	Workers int
}

var _ dagsync.DiscoveryAlgorithm[string, any] = (*Algorithm[string, any])(nil)

func (a *Algorithm[K, V]) Discover(
	ctx context.Context,
	roots []K,
	graph *dagsync.SyncedDirectedAcyclicGraph[K],
	resolver dagsync.Resolver[K, V],
	discoverer dagsync.Discoverer[K, V],
) error {
	workers := a.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	sem := semaphore.NewWeighted(int64(workers))

	var (
		queued   sync.Map
		wg       sync.WaitGroup
		firstErr error
		errMu    sync.Mutex
	)

	setErr := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
		cancel(err)
	}

	// enqueue schedules key for resolution. If key was already queued it only
	// adds the edge parent→key (enabling cycle detection via HasCycle).
	var enqueue func(key K, parent *K, index int)
	enqueue = func(key K, parent *K, index int) {
		_, alreadyQueued := queued.LoadOrStore(key, struct{}{})

		if !alreadyQueued {
			// Add vertex immediately so the edge below can reference it.
			if err := graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
				return d.AddVertex(key, map[string]any{
					dagsync.AttributeDiscoveryState: dagsync.DiscoveryStateDiscovering,
				})
			}); err != nil {
				setErr(err)
				return
			}
		}

		// Always add the edge — even for already-queued nodes — so HasCycle fires on cycles.
		if parent != nil {
			idx := index
			p := *parent
			if err := graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
				return d.AddEdge(p, key, map[string]any{dagsync.AttributeOrderIndex: idx})
			}); err != nil {
				setErr(err)
				return
			}
		}

		if alreadyQueued {
			return
		}

		// Spawn a goroutine that waits for a semaphore slot, resolves the vertex,
		// then enqueues children. The goroutine does NOT wait for children —
		// that is what prevents the deadlock.
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := sem.Acquire(ctx, 1); err != nil {
				setErr(err)
				return
			}
			defer sem.Release(1)

			value, err := resolver.Resolve(ctx, key)
			if err != nil {
				graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error { //nolint:errcheck
					d.Vertices[key].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateError
					return nil
				})
				setErr(fmt.Errorf("resolve %v: %w", key, err))
				return
			}

			graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error { //nolint:errcheck
				d.Vertices[key].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateDiscovered
				d.Vertices[key].Attributes[dagsync.AttributeValue] = value
				return nil
			})

			children, err := discoverer.Discover(ctx, value)
			if err != nil {
				graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error { //nolint:errcheck
					d.Vertices[key].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateError
					return nil
				})
				setErr(fmt.Errorf("discover %v: %w", key, err))
				return
			}

			// Enqueue children before marking this vertex complete.
			// enqueue is non-blocking: it spawns goroutines, never waits.
			for i, child := range children {
				enqueue(child, &key, i)
			}

			graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error { //nolint:errcheck
				d.Vertices[key].Attributes[dagsync.AttributeDiscoveryState] = dagsync.DiscoveryStateCompleted
				return nil
			})
		}()
	}

	for _, root := range roots {
		enqueue(root, nil, 0)
	}

	wg.Wait()

	errMu.Lock()
	defer errMu.Unlock()
	return firstErr
}
