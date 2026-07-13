// Command s3originpoc is a throwaway proof-of-concept for open-component-model/ocm-project#1182.
//
// It demonstrates the "reserved label" carrier for preserving S3 origin across an
// airgapped  s3 -> localBlob -> s3 -> localBlob  transfer round-trip, using the REAL
// OCM descriptor + normalisation code from this repo.
//
// It proves the two claims that actually matter for the label approach:
//
//  1. Signing safety: stamping a reserved, non-signed (signing:false) origin label does
//     NOT change the signed/normalised digest of the component descriptor — while a
//     signing:true label WOULD (the footgun we must avoid).
//  2. Round-trip + multi-hop: the origin survives localisation, is read back and used to
//     reconstruct a real s3 access on export, is stripped afterwards, and never
//     duplicates across a second hop (replace-not-append).
//
// Run:  go run ./s3originpoc   (from bindings/go/descriptor/normalisation)
// This directory is a PoC scaffold and is meant to be deleted afterwards.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	norm "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v4 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	desc "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OriginLabelName is the reserved, DNS-namespaced label that carries transport origin.
// It is machine-owned transport metadata, so it MUST be signing:false.
const OriginLabelName = "software.ocm/origin"

// s3Access is a stand-in for the (greenfield) bindings/go/s3 access type. In the real
// implementation this is a proper runtime.Typed struct; here a Raw is enough to prove the flow.
func s3Access(bucket, key, region, version string) *runtime.Raw {
	data, _ := json.Marshal(map[string]any{
		"type":       "S3/v1",
		"bucketName": bucket,
		"objectKey":  key,
		"region":     region,
		"version":    version,
		"mediaType":  "application/octet-stream",
	})
	r := &runtime.Raw{Data: data}
	r.SetType(runtime.NewVersionedType("S3", "v1"))
	return r
}

func localBlobAccess(localRef string) *runtime.Raw {
	data, _ := json.Marshal(map[string]any{
		"type":           "LocalBlob/v1",
		"localReference": localRef,
		"mediaType":      "application/octet-stream",
	})
	r := &runtime.Raw{Data: data}
	r.SetType(runtime.NewVersionedType("LocalBlob", "v1"))
	return r
}

// --- the carrier logic: the ~three functions the transfer layer would call ---

// stampOrigin localises a resource: it rewrites the access to a localBlob and records the
// ORIGINAL access in a reserved, non-signed label. It REPLACES any existing origin label
// rather than appending, so repeated hops never duplicate it.
func stampOrigin(res *desc.Resource, origin runtime.Typed, localRef string, signing bool) {
	res.Access = localBlobAccess(localRef)

	value, err := json.Marshal(origin)
	if err != nil {
		log.Fatalf("marshal origin: %v", err)
	}
	label := desc.Label{Name: OriginLabelName, Value: value, Signing: signing}

	res.Labels = replaceLabel(res.Labels, label)
}

// readOrigin returns the recorded origin access, if any.
func readOrigin(res *desc.Resource) (*runtime.Raw, bool) {
	for i := range res.Labels {
		if res.Labels[i].Name == OriginLabelName {
			raw := &runtime.Raw{}
			if err := json.Unmarshal(res.Labels[i].Value, raw); err != nil {
				log.Fatalf("unmarshal origin: %v", err)
			}
			return raw, true
		}
	}
	return nil, false
}

// stripOrigin removes the origin label (called on export once the real access is restored).
func stripOrigin(res *desc.Resource) {
	out := res.Labels[:0]
	for _, l := range res.Labels {
		if l.Name != OriginLabelName {
			out = append(out, l)
		}
	}
	res.Labels = out
}

func replaceLabel(labels []desc.Label, l desc.Label) []desc.Label {
	for i := range labels {
		if labels[i].Name == l.Name {
			labels[i] = l
			return labels
		}
	}
	return append(labels, l)
}

func countOrigin(res *desc.Resource) int {
	n := 0
	for _, l := range res.Labels {
		if l.Name == OriginLabelName {
			n++
		}
	}
	return n
}

// --- descriptor scaffolding + normalisation ---

func newDescriptor(res desc.Resource) *desc.Descriptor {
	d := &desc.Descriptor{}
	d.Meta.Version = "v2"
	d.Component.Name = "acme.org/airgap/demo"
	d.Component.Version = "1.0.0"
	d.Component.Provider = desc.Provider{Name: "acme"}
	d.Component.Resources = []desc.Resource{res}
	return d
}

func signedDigest(d *desc.Descriptor) string {
	b, err := norm.Normalise(d, v4.Algorithm)
	if err != nil {
		log.Fatalf("normalise: %v", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func baseResource() desc.Resource {
	var r desc.Resource
	r.Name = "blob"
	r.Version = "1.0.0"
	r.Type = "blob"
	r.Relation = desc.LocalRelation
	r.Access = s3Access("origin-bucket", "artifacts/blob.bin", "eu-central-1", "v-abc123")
	r.Digest = &desc.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  "0000000000000000000000000000000000000000000000000000000000000000",
	}
	return r
}

func check(cond bool, format string, args ...any) {
	if !cond {
		log.Fatalf("ASSERT FAILED: "+format, args...)
	}
	fmt.Printf("  ✓ "+format+"\n", args...)
}

func main() {
	fmt.Println("=== S3 origin via reserved label — airgapped round-trip PoC ===")

	// ---- Baseline: the resource still has its real s3 access, before any transfer.
	res := baseResource()
	d0 := signedDigest(newDescriptor(res))
	fmt.Printf("\n[0] baseline signed digest (s3 access): %s\n", d0[:16])

	// ---- Hop 1: localise into an airgap CTF. Access s3 -> localBlob; stamp origin (signing:false).
	fmt.Println("\n[1] localise  s3 -> localBlob  (stamp origin, signing:false)")
	stampOrigin(&res, s3Access("origin-bucket", "artifacts/blob.bin", "eu-central-1", "v-abc123"), "sha256:deadbeef", false)
	d1 := signedDigest(newDescriptor(res))
	check(res.Access.GetType().String() == "LocalBlob/v1", "access rewritten to localBlob")
	check(countOrigin(&res) == 1, "exactly one origin label present")
	check(d1 == d0, "signed digest UNCHANGED by localise (%s == %s)", d1[:8], d0[:8])

	// ---- The footgun: prove a signing:TRUE origin label would break signature stability.
	fmt.Println("\n[!] footgun check: same label but signing:TRUE")
	bad := baseResource()
	stampOrigin(&bad, s3Access("origin-bucket", "artifacts/blob.bin", "eu-central-1", "v-abc123"), "sha256:deadbeef", true)
	dBad := signedDigest(newDescriptor(bad))
	check(dBad != d0, "signing:true origin label CHANGES the digest (%s != %s) — must stay false", dBad[:8], d0[:8])

	// ---- Hop 2: export/import into a DIFFERENT s3 environment. Read origin, restore real
	//      s3 access (new bucket allowed), strip the label.
	fmt.Println("\n[2] export  localBlob -> s3  (read origin, restore access, strip label)")
	origin, ok := readOrigin(&res)
	check(ok, "origin label found on localBlob")
	var o map[string]any
	_ = json.Unmarshal(origin.Data, &o)
	check(o["bucketName"] == "origin-bucket" && o["objectKey"] == "artifacts/blob.bin" && o["region"] == "eu-central-1",
		"origin s3 coordinates recovered intact: %v/%v (%v)", o["bucketName"], o["objectKey"], o["region"])
	// restore a real s3 access in the NEW environment's bucket, then drop the origin marker
	res.Access = s3Access("airgap-bucket", "artifacts/blob.bin", "eu-west-1", "v-new999")
	stripOrigin(&res)
	check(countOrigin(&res) == 0, "origin label stripped after restore")
	check(res.Access.GetType().String() == "S3/v1", "access is a real s3 access again")

	// ---- Hop 3: localise AGAIN (multi-hop). Must not accumulate duplicate origin labels.
	fmt.Println("\n[3] second localise  s3 -> localBlob  (multi-hop: no duplication)")
	stampOrigin(&res, res.Access, "sha256:cafef00d", false)
	check(countOrigin(&res) == 1, "still exactly one origin label after 2nd hop (replace-not-append)")
	origin2, _ := readOrigin(&res)
	var o2 map[string]any
	_ = json.Unmarshal(origin2.Data, &o2)
	check(o2["bucketName"] == "airgap-bucket", "origin now reflects the LATEST location, not a stale one: %v", o2["bucketName"])
	check(signedDigest(newDescriptor(res)) == d0, "signed digest STILL unchanged after 2nd hop")

	fmt.Println("\n=== PoC passed: label carrier is signing-safe, round-trips, and is multi-hop clean ===")
}
