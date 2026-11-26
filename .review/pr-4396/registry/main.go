// Command registry is a stdlib-only, read-only OCI registry for the PR 4396 review scripts.
// It serves one image under every repository name and requires basic auth user1:password1
// for repositories below project1/. It writes its listen address to -addr-file.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

type blob struct {
	mediaType string
	data      []byte
}

func digestOf(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }

func main() {
	addrFile := flag.String("addr-file", "", "file to write the listen address to")
	flag.Parse()

	var layer bytes.Buffer
	tw := tar.NewWriter(&layer)
	content := []byte("hello world\n")
	if err := tw.WriteHeader(&tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(content))}); err != nil {
		log.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		log.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		log.Fatal(err)
	}
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":["` + digestOf(layer.Bytes()) + `"]}}`)

	blobs := map[string]blob{
		digestOf(layer.Bytes()): {"application/vnd.oci.image.layer.v1.tar", layer.Bytes()},
		digestOf(config):        {"application/vnd.oci.image.config.v1+json", config},
	}
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(config), "size": len(config)},
		"layers":        []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": digestOf(layer.Bytes()), "size": layer.Len()}},
	})
	if err != nil {
		log.Fatal(err)
	}
	manifestDigest := digestOf(manifest)
	manifestType := "application/vnd.oci.image.manifest.v1+json"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		log.Printf("%s %s user=%q", r.Method, r.URL.Path, user)
		if r.URL.Path == "/v2/" || r.URL.Path == "/v2" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v2/project1/") {
			if u, p, ok := r.BasicAuth(); !ok || u != "user1" || p != "password1" {
				w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		var (
			data      []byte
			mediaType string
			dgst      string
		)
		switch idx := strings.LastIndex(r.URL.Path, "/manifests/"); {
		case idx >= 0:
			ref := r.URL.Path[idx+len("/manifests/"):]
			if ref != "1.0.0" && ref != manifestDigest {
				http.NotFound(w, r)
				return
			}
			data, mediaType, dgst = manifest, manifestType, manifestDigest
		case strings.Contains(r.URL.Path, "/blobs/"):
			d := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			b, ok := blobs[d]
			if !ok {
				http.NotFound(w, r)
				return
			}
			data, mediaType, dgst = b.data, b.mediaType, d
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Docker-Content-Digest", dgst)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*addrFile, []byte(ln.Addr().String()), 0o600); err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.Serve(ln, handler))
}
