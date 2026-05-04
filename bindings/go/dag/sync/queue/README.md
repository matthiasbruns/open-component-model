# queue — Bounded-Concurrency Queue-Based Discovery Algorithm

The `queue` package implements a bounded-concurrency graph discovery algorithm for
`ocm.software/open-component-model/bindings/go/dag/sync` that fixes the two
known issues in the `spawn` algorithm (see
[ocm-project#735](https://github.com/open-component-model/ocm-project/issues/735)).

## How it works

A semaphore with `Workers` slots limits how many vertices are actively resolved
at once. Each vertex is processed by a goroutine that:

1. Acquires a semaphore slot.
2. Resolves the vertex value (`Resolver.Resolve`).
3. Discovers child vertex keys (`Discoverer.Discover`).
4. Enqueues each child (spawns a new goroutine per child, non-blocking).
5. Releases the semaphore slot and returns.

**The parent goroutine never waits for its children.** This is the key
difference from the spawn algorithm and is why a concurrency limit does not
cause a deadlock.

```
semaphore (Workers = 2)

A ──acquire──► resolve+discover ──release──► spawn B, spawn C
                                             (A goroutine exits)
B ──acquire──► resolve+discover ──release──► spawn D
C ──acquire──► resolve+discover ──release──► (D already queued — only adds edge)
D ──acquire──► resolve+discover ──release──► (no children)
```

## Cycle detection

When a child node is already queued, the algorithm still calls
`dag.AddEdge(parent, child)`. The underlying DAG runs `HasCycle()` on every
`AddEdge` call, so cycles are detected and returned as errors.

## Configuration

```go
&queue.Algorithm[K, V]{
    Workers: 4, // 0 or negative → runtime.GOMAXPROCS(0)
}
```

## Characteristics

| Property            | Behaviour                                         |
|---------------------|---------------------------------------------------|
| Concurrency model   | Semaphore-bounded goroutines                      |
| Concurrency limit   | `Workers` (default: `runtime.GOMAXPROCS(0)`)     |
| Cycle detection     | Via `dag.DirectedAcyclicGraph.AddEdge`            |
| Deduplication       | `sync.Map` (LoadOrStore per key)                  |
| Deadlock risk       | None — parents never wait for children            |

## Comparison with spawn

| Feature                     | spawn        | queue                  |
|-----------------------------|--------------|------------------------|
| Bounded concurrency         | No           | Yes (`Workers`)        |
| Deadlock with limit         | Yes          | No                     |
| Goroutines per vertex       | 1 + children | 1 (semaphore-queued)   |
| Suitable for deep graphs    | Risky        | Yes                    |

## When to use

- You need to bound memory or I/O parallelism.
- Graph depth is unknown or potentially large.
- Production workloads where goroutine explosion is unacceptable.
