#!/bin/sh
# Real CLI + filesystem CTF observation; requires Go and Python 3, no review tests.
# Post-processing validation is intentional; no no-upload requirement is assumed.
# Exit 0: side effects observed and controls passed; 2: setup/unexpected failure.
set -eu
exec python3 - "$0" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve().parent
module = root / "bindings/go"
env = dict(os.environ, OCM_DISABLE_VERSION_CHECK="1")


def interrupted(signum, frame):
    raise RuntimeError("interrupted or overall 300-second deadline exceeded")


signal.signal(signal.SIGTERM, interrupted)
signal.signal(signal.SIGALRM, interrupted)
signal.alarm(300)

# Observe the real CTF through the binding's filesystem and public CTF APIs.
INSPECTOR = r'''
package main
import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "io/fs"
    "os"
    "ocm.software/open-component-model/bindings/go/blob/filesystem"
    "ocm.software/open-component-model/bindings/go/ctf"
)
func main() {
    if err := inspect(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(2) }
}
func inspect() error {
    f, err := filesystem.NewFS(os.Args[1], os.O_RDONLY)
    if err != nil { return err }
    archive := ctf.NewFileSystemCTF(f)
    ctx := context.Background()
    index, err := archive.GetIndex(ctx)
    if err != nil { return err }
    blobs, err := archive.ListBlobs(ctx)
    if err != nil && !errors.Is(err, fs.ErrNotExist) { return err }
    // Read every listed blob as well: a filename alone is not proof of an upload.
    contents := map[string]string{}
    for _, digest := range blobs {
        b, err := archive.GetBlob(ctx, digest)
        if err != nil { return err }
        reader, err := b.ReadCloser()
        if err != nil { return err }
        data, readErr := io.ReadAll(reader)
        closeErr := reader.Close()
        if err := errors.Join(readErr, closeErr); err != nil { return err }
        contents[digest] = string(data)
    }
    return json.NewEncoder(os.Stdout).Encode(map[string]any{"index": index, "blobs": contents})
}
'''

try:
    with tempfile.TemporaryDirectory(prefix="review-pr3635-uploads-") as directory:
        work = Path(directory)
        inspector = work / "inspect.go"
        inspector.write_text(INSPECTOR)
        config = work / "config.json"
        config.write_text(json.dumps({"type": "generic.config.ocm.software/v1", "configurations": []}))

        def run(command, cwd):
            return subprocess.run(command, cwd=cwd, env=env, capture_output=True,
                                  text=True, timeout=120)

        def snapshot(repo):
            result = run(["go", "run", str(inspector), str(repo)], module)
            if result.returncode:
                raise RuntimeError(f"CTF inspection failed: {result.stdout}{result.stderr}")
            return json.loads(result.stdout)

        for kind in ("resources", "sources"):
            for version in ("1.0.0", "not-a-version"):
                label = f"{kind}-{version}"
                repo = work / ("ctf-" + label)
                repo.mkdir()
                before = snapshot(repo)
                payload = "review-pr3635 payload " + label
                data = work / (label + ".txt")
                data.write_text(payload)
                constructor = work / (label + ".json")
                constructor.write_text(json.dumps({"components": [{
                    "name": "example.org/review-pr3635", "version": "1.0.0",
                    "provider": {"name": "example.org"},
                    kind: [{"name": "artifact", "version": version, "type": "blob",
                            "input": {"type": "file/v1", "path": str(data)}}],
                }]}))
                result = run(["go", "run", "main.go", "--config", str(config),
                              "add", "component-version", "--repository", f"ctf::{repo}",
                              "--constructor", str(constructor)], module / "cli")
                after = snapshot(repo)
                digest = "sha256:" + hashlib.sha256(payload.encode()).hexdigest()
                uploaded = after["blobs"].get(digest) == payload
                if version == "1.0.0":
                    if result.returncode or not uploaded or after["index"] == before["index"]:
                        raise RuntimeError(f"{label}: valid control failed: {result.stdout}{result.stderr}")
                    print(f"PASS {label}: CLI committed descriptor and readable input blob", flush=True)
                    continue
                output = result.stdout + result.stderr
                if not result.returncode or 'invalid version "not-a-version"' not in output:
                    raise RuntimeError(f"{label}: expected version rejection, got: {output}")
                if after["index"] != before["index"]:
                    raise RuntimeError(f"{label}: descriptor index unexpectedly changed")
                if uploaded:
                    print(f"OBSERVATION {label}: version rejected, no descriptor committed, but input blob persists ({digest})", flush=True)
                elif after["blobs"] != before["blobs"]:
                    raise RuntimeError(f"{label}: unexpected blobs rather than the known orphan payload")
                else:
                    print(f"PASS {label}: rejected before upload", flush=True)
        print("OBSERVATION COMPLETE: early validation of explicit versions is an improvement question, not an asserted atomicity contract.")
        sys.exit(0)
except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
    print(f"ERROR (not a confirmed regression): {error}", file=sys.stderr)
    sys.exit(2)
finally:
    signal.alarm(0)
PY
