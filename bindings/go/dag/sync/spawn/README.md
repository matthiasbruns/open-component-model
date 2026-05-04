# spawn — Recursive Spawn Discovery Algorithm

The `spawn` package implements the original graph discovery algorithm for
`ocm.software/open-component-model/bindings/go/dag/sync`.

## How it works

Discovery starts from one or more root vertices. Each vertex is resolved
(via `Resolver`) and its neighbors are expanded (via `Discoverer`). Every
vertex spawns a new goroutine per child, all collected under an `errgroup`.
The parent goroutine waits for all child goroutines before completing.

```
Root A  ──spawn──►  B  ──spawn──►  D
        └─spawn──►  C  ──spawn──►  D (deduped via doneMap)
```

Vertex deduplication is handled by a `sync.Map` of "done" channels. If two
goroutines race to discover the same vertex, the second one blocks on the
channel until the first finishes, then returns without re-processing.

## Characteristics

| Property            | Behaviour                                      |
|---------------------|------------------------------------------------|
| Concurrency model   | One goroutine per vertex (unbounded)           |
| Concurrency limit   | **Not supported** — see below                 |
| Cycle detection     | Via `dag.DirectedAcyclicGraph.AddEdge`         |
| Deduplication       | `sync.Map` of done channels                   |

## Known limitation: concurrency limit causes deadlock

Adding a goroutine limit (e.g. `errgroup.SetLimit(N)`) deadlocks when graph
depth exceeds `N`. A parent goroutine holds a slot while blocking on
`errGroup.Wait()`, preventing its children from acquiring slots.

```
errgroup limit = 3

A (slot 1) ──► B (slot 2) ──► D  ← can't get slot, blocks
└────────────► C (slot 3) ──► D  ← can't get slot, blocks
                                   A and B are waiting → DEADLOCK
```

Use `queue.Algorithm` when you need a configurable concurrency limit.

## When to use

- Graphs are small or depth is bounded.
- Unbounded goroutine spawning is acceptable.
- You want the simplest possible implementation.
