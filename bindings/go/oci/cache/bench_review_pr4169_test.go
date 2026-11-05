package cache

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry"
)

// Benchmarks for the served-record write that [RemotePolicyAlways] performs on
// every first read of a blob in a repository ([BlobCache.fetchAlways] ->
// [ReferenceCache.Add] -> writeNamespace).
//
// writeNamespace has two costs, and they scale with different things:
//
//   - snapshotNamespace iterates ReferenceCache.lru.Keys(), which returns every
//     key of every namespace. That scan is O(all records in the cache).
//   - the JSON marshal and the fsynced rewrite are O(records in the one
//     namespace being written).
//
// The grid varies both independently so the two are not conflated. The served
// cache's entry cap is Options.MaxEntries, which NewBlobCache passes through
// from the blob cache (DefaultBlobMaxEntries = 100_000), so the large end of
// the grid is the configured worst case rather than an extrapolation.
//
// Run from bindings/go. The two end-to-end benchmarks each do real disk I/O per
// iteration, so cap their iteration count:
//
//	go test ./oci/cache/ -run '^$' -bench 'ReviewPR4169_(ServedRecordAdd|ColdReadByPolicy)' -benchtime 20x -timeout 30m
//
// BenchmarkReviewPR4169_WriteCostBreakdown is a microbenchmark; let Go pick the
// iteration count, because 20 iterations is too few to be stable:
//
//	go test ./oci/cache/ -run '^$' -bench ReviewPR4169_WriteCostBreakdown

const benchNamespaceFormat = "component-descriptors/example.com/component-%d"

// seedServedCache builds a served-record cache holding namespaces *
// perNamespace records and returns it with the namespace the benchmark writes
// to. MaxEntries is left unlimited so the seed cannot be evicted mid-run.
func seedServedCache(b *testing.B, namespaces, perNamespace int) (*ReferenceCache, registry.Reference) {
	b.Helper()
	rc, err := NewReferenceCache(Options{Dir: b.TempDir(), MaxEntries: -1, TTL: noExpiry})
	if err != nil {
		b.Fatal(err)
	}
	var hot registry.Reference
	for n := range namespaces {
		ref := registry.Reference{
			Registry:   "registry.example",
			Repository: fmt.Sprintf(benchNamespaceFormat, n),
		}
		for i := range perNamespace {
			ref.Reference = benchDigest(i).String()
			rc.lru.Add(rc.buildKey(ref), referenceEntry{SavedAt: time.Now()})
		}
		if n == 0 {
			hot = ref
		}
	}
	// Materialise the hot namespace's snapshot file so the first timed Add does
	// not pay for creating it.
	hot.Reference = benchDigest(perNamespace).String()
	rc.Add(hot, ociImageSpecV1.Descriptor{Digest: digest.Digest(hot.Reference)})
	return rc, hot
}

func benchDigest(i int) digest.Digest {
	return digest.Digest(fmt.Sprintf("sha256:%064x", i))
}

// snapshotSize reports the size of the hot namespace's snapshot file, i.e. how
// many bytes one Add rewrites and fsyncs.
func snapshotSize(b *testing.B, rc *ReferenceCache, ref registry.Reference) int64 {
	b.Helper()
	fi, err := os.Stat(rc.pathForNamespace(rc.buildKey(ref).namespace))
	if err != nil {
		b.Fatal(err)
	}
	return fi.Size()
}

// BenchmarkReviewPR4169_ServedRecordAdd measures one served-record write at a
// range of cache shapes. The reported rewrite_B metric is the snapshot the
// write rewrites and fsyncs; scan_records is how many keys snapshotNamespace
// walks to produce it.
func BenchmarkReviewPR4169_ServedRecordAdd(b *testing.B) {
	shapes := []struct {
		namespaces, perNamespace int
	}{
		// Spread wide: many components, few versions each. Isolates the
		// all-namespaces Keys() scan; each rewrite is tiny.
		{1, 100},
		{1_000, 100},
		{10_000, 10},
		// Concentrated: one long-lived component with many versions. Both the
		// scan and the rewrite grow together.
		{1, 1_000},
		{1, 10_000},
		{1, 50_000},
		{1, 100_000},
	}
	for _, s := range shapes {
		name := fmt.Sprintf("namespaces=%d/records_each=%d", s.namespaces, s.perNamespace)
		b.Run(name, func(b *testing.B) {
			rc, hot := seedServedCache(b, s.namespaces, s.perNamespace)
			key := rc.buildKey(hot)
			total := s.namespaces * s.perNamespace

			b.ResetTimer()
			for i := range b.N {
				hot.Reference = benchDigest(s.perNamespace + 1 + i).String()
				rc.Add(hot, ociImageSpecV1.Descriptor{Digest: digest.Digest(hot.Reference)})
				// Drop it again (no disk write) so the namespace size, and with
				// it the cost being measured, stays constant across iterations.
				b.StopTimer()
				k := key
				k.reference = hot.Reference
				rc.lru.Remove(k)
				b.StartTimer()
			}
			b.StopTimer()
			b.ReportMetric(float64(snapshotSize(b, rc, hot)), "rewrite_B")
			b.ReportMetric(float64(total), "scan_records")
		})
	}
}

// BenchmarkReviewPR4169_ColdReadByPolicy reads n distinct blobs once each from
// one repository, through the full BlobCache.Fetch path, against an in-memory
// upstream. The only per-fetch difference between the two policies is the
// served-record write, so the ratio is the local cost RemotePolicyAlways adds
// to a cold read.
func BenchmarkReviewPR4169_ColdReadByPolicy(b *testing.B) {
	const n = 500
	for _, policy := range []RemotePolicy{RemotePolicyIfNotPresent, RemotePolicyAlways} {
		b.Run(string(policy), func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				c, err := NewBlobCache(Options{
					Dir:          filepath.Join(b.TempDir(), "blobcache"),
					RemotePolicy: policy,
				})
				if err != nil {
					b.Fatal(err)
				}
				repo := newFakeRepository("registry.example", "component-descriptors/example.com/component")
				descs := make([]ociImageSpecV1.Descriptor, 0, n)
				for i := range n {
					descs = append(descs, repo.put(ociImageSpecV1.MediaTypeImageManifest,
						[]byte(fmt.Sprintf(`{"schemaVersion":2,"n":%d}`, i))))
				}
				b.StartTimer()

				for _, d := range descs {
					rc, err := c.Fetch(b.Context(), repo, d)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := io.Copy(io.Discard, rc); err != nil {
						b.Fatal(err)
					}
					if err := rc.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.ReportMetric(float64(n), "blobs/op")
		})
	}
}

// writeFileNoSync is writeFileAtomic without the fsync, to measure what
// dropping durability for served records would buy. Records are not
// authoritative: losing one costs an extra upstream fetch, not correctness,
// because a hit also requires the digest-named blob and a passing Exists.
func writeFileNoSync(dir, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, atomicWritePrefix+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// BenchmarkReviewPR4169_WriteCostBreakdown splits one served-record write into
// the parts that make it up, so each proposed remedy can be costed separately.
// The cache shape is the realistic one: many components, few versions each,
// filled to the served cache's default entry cap.
//
//   - snapshot_scan_all_namespaces is what snapshotNamespace does today: walk
//     ReferenceCache.lru.Keys(), which is every key of every namespace, and
//     keep the ones belonging to the namespace being written.
//   - snapshot_from_namespace_index is the same snapshot built from a
//     per-namespace index, i.e. what a namespace-local index would cost.
//   - write_with_fsync is the current writeFileAtomic.
//   - write_without_fsync is the same write with the fsync dropped.
func BenchmarkReviewPR4169_WriteCostBreakdown(b *testing.B) {
	const (
		namespaces   = 1_000
		perNamespace = 100 // 100_000 records total: the default MaxEntries cap
	)
	rc, hot := seedServedCache(b, namespaces, perNamespace)
	namespace := rc.buildKey(hot).namespace

	// A namespace-local index of the same content, as a replacement for the
	// all-namespaces Keys() scan.
	index := make(map[string]map[string]referenceEntry)
	for _, k := range rc.lru.Keys() {
		if v, ok := rc.lru.Peek(k); ok {
			if index[k.namespace] == nil {
				index[k.namespace] = make(map[string]referenceEntry)
			}
			index[k.namespace][k.reference] = v
		}
	}

	b.Run("snapshot_scan_all_namespaces", func(b *testing.B) {
		for b.Loop() {
			snap := rc.snapshotNamespace(namespace)
			if len(snap.References) == 0 {
				b.Fatal("empty snapshot")
			}
		}
	})

	b.Run("snapshot_from_namespace_index", func(b *testing.B) {
		for b.Loop() {
			snap := referenceFileSnapshot{Namespace: namespace, References: make(map[string]referenceEntry, len(index[namespace]))}
			for ref, entry := range index[namespace] {
				snap.References[ref] = entry
			}
			if len(snap.References) == 0 {
				b.Fatal("empty snapshot")
			}
		}
	})

	raw, err := json.Marshal(rc.snapshotNamespace(namespace))
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("snapshot of one namespace at %d total records: %d B", namespaces*perNamespace, len(raw))
	dir := b.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	b.Run("write_with_fsync", func(b *testing.B) {
		for b.Loop() {
			if err := writeFileAtomic(dir, path, raw); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("write_without_fsync", func(b *testing.B) {
		for b.Loop() {
			if err := writeFileNoSync(dir, path, raw); err != nil {
				b.Fatal(err)
			}
		}
	})
}
