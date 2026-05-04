package sync

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/dag"
)

type DiscoveryState int

func (s DiscoveryState) String() string {
	switch s {
	case DiscoveryStateDiscovering:
		return "discovering"
	case DiscoveryStateDiscovered:
		return "discovered"
	case DiscoveryStateCompleted:
		return "completed"
	case DiscoveryStateError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

const (
	AttributeValue          = "dag/value"
	AttributeDiscoveryState = "dag/discovery-state"
	AttributeOrderIndex     = "dag/order-index"
)

const (
	DiscoveryStateUnknown DiscoveryState = iota
	DiscoveryStateDiscovering
	DiscoveryStateDiscovered
	DiscoveryStateCompleted
	DiscoveryStateError
)

// DiscoveryAlgorithm is the interface for pluggable graph discovery strategies.
// Implementations build the graph by resolving vertices and discovering neighbors.
// They must be safe to call concurrently, add every visited vertex and edge to graph,
// return an error on cycles or resolution failure, and respect ctx cancellation.
type DiscoveryAlgorithm[K cmp.Ordered, V any] interface {
	Discover(ctx context.Context, roots []K, graph *SyncedDirectedAcyclicGraph[K], resolver Resolver[K, V], discoverer Discoverer[K, V]) error
}

func NewGraphDiscoverer[K cmp.Ordered, V any](opts *GraphDiscovererOptions[K, V]) *GraphDiscoverer[K, V] {
	return &GraphDiscoverer[K, V]{
		graph: NewSyncedDirectedAcyclicGraph[K](),
		opts:  opts,
	}
}

type GraphDiscoverer[K cmp.Ordered, V any] struct {
	opts  *GraphDiscovererOptions[K, V]
	graph *SyncedDirectedAcyclicGraph[K]
}

func (d *GraphDiscoverer[K, V]) Graph() *SyncedDirectedAcyclicGraph[K] {
	return d.graph
}

func (d *GraphDiscoverer[K, V]) CurrentEdges(key K) []K {
	var edges []K
	_ = d.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		v, ok := d.Vertices[key]
		if !ok {
			return nil
		}
		edges = make([]K, len(v.Edges))
		for k, edge := range v.Edges {
			if idx, ok := edge[AttributeOrderIndex]; ok {
				edges[idx.(int)] = k
			} else {
				panic("edges without order index should never exist")
			}
		}
		return nil
	})
	return edges
}

func (d *GraphDiscoverer[K, V]) CurrentValue(key K) V {
	var value V
	_ = d.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		v, ok := d.Vertices[key]
		if !ok {
			return nil
		}
		value, _ = v.Attributes[AttributeValue].(V)
		return nil
	})
	return value
}

func (d *GraphDiscoverer[K, V]) CurrentState(key K) DiscoveryState {
	state := DiscoveryStateUnknown
	_ = d.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		v, ok := d.Vertices[key]
		if !ok {
			state = DiscoveryStateUnknown
			return nil
		}
		s, ok := v.Attributes[AttributeDiscoveryState]
		if !ok {
			panic("vertex without discovery state should never exist")
		}
		state = s.(DiscoveryState)
		return nil
	})
	return state
}

func (d *GraphDiscoverer[K, V]) Discover(ctx context.Context) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = errors.Join(retErr, fmt.Errorf("discovery panicked: %v", r))
		}
	}()

	if len(d.opts.Roots) == 0 {
		return fmt.Errorf("no roots provided, cannot discover")
	}
	if d.opts.Algorithm == nil {
		return fmt.Errorf("no discovery algorithm configured")
	}

	if err := d.opts.Algorithm.Discover(ctx, d.opts.Roots, d.graph, d.opts.Resolver, d.opts.Discoverer); err != nil {
		return fmt.Errorf("failed to discover graph: %w", err)
	}
	return nil
}
