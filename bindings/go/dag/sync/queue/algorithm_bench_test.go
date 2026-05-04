package queue_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/dag/sync/queue"
)

func buildChainGraph(n int) map[int][]int {
	g := make(map[int][]int, n)
	for i := 0; i < n-1; i++ {
		g[i] = []int{i + 1}
	}
	g[n-1] = nil
	return g
}

func buildWideGraph(n int) map[int][]int {
	g := make(map[int][]int, n+1)
	children := make([]int, n)
	for i := 0; i < n; i++ {
		children[i] = i + 1
		g[i+1] = nil
	}
	g[0] = children
	return g
}

func buildDiamondGraph(depth, width int) map[int][]int {
	g := make(map[int][]int)
	id := 0
	prev := []int{id}
	id++
	for d := 0; d < depth; d++ {
		cur := make([]int, width)
		for w := 0; w < width; w++ {
			cur[w] = id
			g[id] = nil
			id++
		}
		for _, p := range prev {
			g[p] = append(g[p], cur...)
		}
		prev = cur
	}
	return g
}

func runQueueBench(b *testing.B, workers int, graph map[int][]int, roots []int) {
	b.Helper()
	resolver := dagsync.ResolverFunc[int, int](func(_ context.Context, key int) (int, error) {
		return key, nil
	})
	discoverer := dagsync.DiscovererFunc[int, int](func(_ context.Context, parent int) ([]int, error) {
		return graph[parent], nil
	})
	b.ResetTimer()
	for range b.N {
		d := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[int, int]{
			Algorithm:  &queue.Algorithm[int, int]{Workers: workers},
			Roots:      roots,
			Resolver:   resolver,
			Discoverer: discoverer,
		})
		if err := d.Discover(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

var defaultWorkers = runtime.GOMAXPROCS(0)

func BenchmarkQueue_Chain10(b *testing.B)   { runQueueBench(b, defaultWorkers, buildChainGraph(10), []int{0}) }
func BenchmarkQueue_Chain100(b *testing.B)  { runQueueBench(b, defaultWorkers, buildChainGraph(100), []int{0}) }
func BenchmarkQueue_Chain1000(b *testing.B) { runQueueBench(b, defaultWorkers, buildChainGraph(1000), []int{0}) }

func BenchmarkQueue_Wide10(b *testing.B)   { runQueueBench(b, defaultWorkers, buildWideGraph(10), []int{0}) }
func BenchmarkQueue_Wide100(b *testing.B)  { runQueueBench(b, defaultWorkers, buildWideGraph(100), []int{0}) }
func BenchmarkQueue_Wide1000(b *testing.B) { runQueueBench(b, defaultWorkers, buildWideGraph(1000), []int{0}) }

func BenchmarkQueue_Diamond_2x5(b *testing.B) {
	runQueueBench(b, defaultWorkers, buildDiamondGraph(2, 5), []int{0})
}
func BenchmarkQueue_Diamond_3x10(b *testing.B) {
	runQueueBench(b, defaultWorkers, buildDiamondGraph(3, 10), []int{0})
}

// Workers=1 variants prove no deadlock under strict serial execution
func BenchmarkQueue_Chain100_Workers1(b *testing.B) {
	runQueueBench(b, 1, buildChainGraph(100), []int{0})
}
func BenchmarkQueue_Wide100_Workers1(b *testing.B) {
	runQueueBench(b, 1, buildWideGraph(100), []int{0})
}

func BenchmarkQueue_DeepWide(b *testing.B) {
	g := make(map[int][]int)
	id := 1
	g[0] = nil
	q := []int{0}
	for depth := 0; depth < 5; depth++ {
		next := make([]int, 0)
		for _, parent := range q {
			children := make([]int, 4)
			for i := range children {
				children[i] = id
				g[id] = nil
				id++
			}
			g[parent] = children
			next = append(next, children...)
		}
		q = next
	}
	b.ResetTimer()
	for range b.N {
		d := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[int, int]{
			Algorithm: &queue.Algorithm[int, int]{Workers: defaultWorkers},
			Roots:     []int{0},
			Resolver: dagsync.ResolverFunc[int, int](func(_ context.Context, key int) (int, error) {
				return key, nil
			}),
			Discoverer: dagsync.DiscovererFunc[int, int](func(_ context.Context, parent int) ([]int, error) {
				return g[parent], nil
			}),
		})
		if err := d.Discover(context.Background()); err != nil {
			b.Fatal(fmt.Sprintf("unexpected error: %v", err))
		}
	}
}
