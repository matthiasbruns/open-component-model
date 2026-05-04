package spawn_test

import (
	"context"
	"fmt"
	"testing"

	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/dag/sync/spawn"
)

// buildChainGraph builds a linear chain: 0→1→2→...→n-1
func buildChainGraph(n int) map[int][]int {
	g := make(map[int][]int, n)
	for i := 0; i < n-1; i++ {
		g[i] = []int{i + 1}
	}
	g[n-1] = nil
	return g
}

// buildWideGraph builds a star: root → n children, no deeper levels
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

// buildDiamondGraph builds a multi-level diamond with shared nodes
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

func runSpawnBench(b *testing.B, graph map[int][]int, roots []int) {
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
			Algorithm:  &spawn.Algorithm[int, int]{},
			Roots:      roots,
			Resolver:   resolver,
			Discoverer: discoverer,
		})
		if err := d.Discover(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSpawn_Chain10(b *testing.B)   { runSpawnBench(b, buildChainGraph(10), []int{0}) }
func BenchmarkSpawn_Chain100(b *testing.B)  { runSpawnBench(b, buildChainGraph(100), []int{0}) }
func BenchmarkSpawn_Chain1000(b *testing.B) { runSpawnBench(b, buildChainGraph(1000), []int{0}) }

func BenchmarkSpawn_Wide10(b *testing.B)   { runSpawnBench(b, buildWideGraph(10), []int{0}) }
func BenchmarkSpawn_Wide100(b *testing.B)  { runSpawnBench(b, buildWideGraph(100), []int{0}) }
func BenchmarkSpawn_Wide1000(b *testing.B) { runSpawnBench(b, buildWideGraph(1000), []int{0}) }

func BenchmarkSpawn_Diamond_2x5(b *testing.B) {
	g := buildDiamondGraph(2, 5)
	runSpawnBench(b, g, []int{0})
}
func BenchmarkSpawn_Diamond_3x10(b *testing.B) {
	g := buildDiamondGraph(3, 10)
	runSpawnBench(b, g, []int{0})
}

func BenchmarkSpawn_MultiRoot(b *testing.B) {
	n := 100
	g := buildWideGraph(n)
	roots := make([]int, n)
	for i := range roots {
		roots[i] = i + 1
		g[i+1] = nil
	}
	// add a shared sink
	sink := n + 1
	for i := range roots {
		g[roots[i]] = []int{sink}
	}
	g[sink] = nil
	runSpawnBench(b, g, roots)
}

func BenchmarkSpawn_DeepWide(b *testing.B) {
	// 5 levels, each branching into 4 unique children — no sharing
	g := make(map[int][]int)
	id := 1
	g[0] = nil
	queue := []int{0}
	for depth := 0; depth < 5; depth++ {
		next := make([]int, 0)
		for _, parent := range queue {
			children := make([]int, 4)
			for i := range children {
				children[i] = id
				g[id] = nil
				id++
			}
			g[parent] = children
			next = append(next, children...)
		}
		queue = next
	}
	b.ResetTimer()
	for range b.N {
		d := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[int, int]{
			Algorithm: &spawn.Algorithm[int, int]{},
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
