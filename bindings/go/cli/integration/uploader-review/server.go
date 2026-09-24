//go:build ignore

// Run with go run server.go --dir ARTIFACTS; no module dependencies are needed.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func sha(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

type object struct {
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
	File   string `json:"file"`
}

func main() {
	dir := flag.String("dir", "", "retained artifact directory (required)")
	flag.Parse()
	if *dir == "" {
		log.Fatal("--dir is required")
	}
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		log.Fatal(err)
	}
	events, err := os.OpenFile(filepath.Join(*dir, "requests.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	defer events.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	const media = "application/vnd.oci.image."
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": media + "manifest.v1+json",
		"config": map[string]any{"mediaType": media + "config.v1+json", "size": len(config), "digest": "sha256:" + sha(config)},
		"layers": []any{},
	})
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	objects := map[string]object{}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 10 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/shutdown" {
			w.WriteHeader(http.StatusOK)
			cancel()
			return
		}
		if path == "/state" {
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{"objects": objects, "manifest_sha256": sha(manifest)}); err != nil {
				log.Print(err)
			}
			return
		}
		var body []byte
		var readErr error
		if path != "/early" {
			body, readErr = io.ReadAll(r.Body)
		}
		mu.Lock()
		err := json.NewEncoder(events).Encode(map[string]any{
			"method": r.Method, "path": r.URL.RequestURI(), "headers": r.Header,
			"body_size": len(body), "body_sha256": sha(body), "body_consumed": path != "/early" && readErr == nil,
		})
		mu.Unlock()
		if err != nil {
			log.Print(err)
			http.Error(w, "event write failed", http.StatusInternalServerError)
			return
		}
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusBadRequest)
			return
		}
		reply := func(data []byte, contentType string) {
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			if r.Method != http.MethodHead {
				_, _ = w.Write(data)
			}
		}
		switch {
		case path == "/early" || path == "/malformed":
			// Hijacking prevents net/http from draining an unread request body.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				log.Print(err)
				return
			}
			defer conn.Close()
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			response := "not an HTTP response\r\n\r\n"
			if path == "/early" {
				response = "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
			}
			_, _ = io.WriteString(conn, response)
		case path == "/source/blob":
			reply([]byte("original resource bytes\n"), "application/octet-stream")
		case path == "/source/large":
			reply([]byte(strings.Repeat("x", 32<<20)), "application/octet-stream")
		case path == "/redirect":
			w.Header().Set("Location", "/page")
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusFound)
		case path == "/page":
			reply([]byte("ordinary GET page; nothing was stored\n"), "text/plain")
		case strings.HasPrefix(path, "/target/") && r.Method == http.MethodPut:
			filename := "target-" + sha([]byte(path)) + ".bin"
			mu.Lock()
			err := os.WriteFile(filepath.Join(*dir, filename), body, 0o600)
			if err == nil {
				objects[path] = object{Size: len(body), SHA256: sha(body), File: filename}
			}
			mu.Unlock()
			if err != nil {
				log.Print(err)
				http.Error(w, "store failed", http.StatusInternalServerError)
				return
			}
			reply(nil, "application/octet-stream")
		case strings.HasPrefix(path, "/target/") && (r.Method == http.MethodGet || r.Method == http.MethodHead):
			mu.Lock()
			obj, exists := objects[path]
			var data []byte
			var err error
			if exists {
				data, err = os.ReadFile(filepath.Join(*dir, obj.File))
			}
			mu.Unlock()
			if !exists {
				http.NotFound(w, r)
			} else if err != nil {
				http.Error(w, "read failed", http.StatusInternalServerError)
			} else {
				reply(data, "application/octet-stream")
			}
		case (path == "/v2" || strings.HasPrefix(path, "/v2/")) && (r.Method == http.MethodGet || r.Method == http.MethodHead):
			w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			switch path {
			case "/v2", "/v2/":
				reply([]byte("{}"), "application/json")
			case "/v2/review/image/manifests/latest", "/v2/review/image/manifests/sha256:" + sha(manifest):
				w.Header().Set("Docker-Content-Digest", "sha256:"+sha(manifest))
				reply(manifest, media+"manifest.v1+json")
			case "/v2/review/image/blobs/sha256:" + sha(config):
				w.Header().Set("Docker-Content-Digest", "sha256:"+sha(config))
				reply(config, media+"config.v1+json")
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
	if err := os.WriteFile(filepath.Join(*dir, "url"), []byte("http://"+listener.Addr().String()+"\n"), 0o600); err != nil {
		log.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Print(err)
		cancel()
	}
	<-done
}
