#!/bin/sh
# PR 3635 finding 6; standalone CLI repro, requiring only Go and Python 3.
# Exit 0: active scheme displayed; 1: confirmed omission; 2: setup/CLI failure.
set -eu
command -v python3 >/dev/null 2>&1 || { echo 'ERROR: Python 3 required' >&2; exit 2; }
command -v go >/dev/null 2>&1 || { echo 'ERROR: Go required' >&2; exit 2; }
exec python3 - "$0" <<'PY'
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve().parent
cli_dir = root / "bindings/go/cli"
env = dict(os.environ, OCM_DISABLE_VERSION_CHECK="1")
component = "example.org/review-pr3635"


def interrupted(signum, frame):
    raise RuntimeError("interrupted or overall 300-second deadline exceeded")


def ocm(config, *args):
    command = ["go", "run", "main.go", "--config", str(config), *args]
    with subprocess.Popen(command, cwd=cli_dir, env=env, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, text=True, start_new_session=True) as process:
        try:
            stdout, stderr = process.communicate(timeout=120)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
    if process.returncode:
        raise RuntimeError(f"CLI failed ({process.returncode}): {args!r}\n{stdout}{stderr}")
    return stdout


for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGALRM):
    signal.signal(sig, interrupted)
signal.alarm(300)
try:
    with tempfile.TemporaryDirectory(prefix="review-pr3635-effective-") as directory:
        work = Path(directory)
        config = work / "config.json"
        versioning = {
            "type": "versioning.config.ocm.software/v1alpha1",
            "schemes": [{"name": "review-build", "pattern": r"^build-(?P<n>[0-9]+)$",
                         "comparisonGroups": ["n"]}],
        }
        config.write_text(json.dumps({"type": "generic.config.ocm.software/v1",
                                      "configurations": [versioning]}))
        constructor = work / "constructor.json"
        constructor.write_text(json.dumps({"components": [
            {"name": component, "version": "build-10", "provider": {"name": "example.org"}}
        ]}))
        repo = work / "repository"
        # Non-semver version: successful add and constrained get prove the scheme is active.
        ocm(config, "add", "component-version", "--repository", f"ctf::{repo}",
            "--constructor", str(constructor))
        listing = json.loads(ocm(config, "get", "component-versions", f"ctf::{repo}//{component}",
                                 "--constraint", ">=build-10", "--output", "json"))
        if not isinstance(listing, list) or [item["component"]["version"] for item in listing] != ["build-10"]:
            raise RuntimeError(f"Configured scheme did not round-trip build-10: {listing!r}")
        print("Configured non-semver scheme accepted: add/get build-10 succeeded.", flush=True)
        raw = ocm(config, "get", "config", "--output", "json")
        effective = json.loads(raw)
        if not isinstance(effective, dict) or effective.get("type") != "generic.config.ocm.software/v1":
            raise RuntimeError(f"Unexpected effective configuration: {raw}")
        entries = effective.get("configurations")
        if not isinstance(entries, list):
            raise RuntimeError(f"Unexpected configurations shape: {raw}")
        print("get config output:\n" + raw, flush=True)
        matching = [entry for entry in entries if entry.get("type") in
                    ("versioning.config.ocm.software/v1alpha1", "versioning.config.ocm.software")]
        if not matching:
            print("FAIL: get config omitted the active versioning configuration")
            sys.exit(1)
        if len(matching) != 1 or matching[0].get("schemes") != versioning["schemes"]:
            raise RuntimeError(f"Versioning entry present but unexpected: {matching!r}")
        print("PASS: get config includes the active versioning scheme")
except (OSError, ValueError, KeyError, TypeError, AttributeError, RuntimeError,
        subprocess.SubprocessError) as error:
    print(f"ERROR (not a confirmed regression): {error}", file=sys.stderr)
    sys.exit(2)
finally:
    signal.alarm(0)
PY
