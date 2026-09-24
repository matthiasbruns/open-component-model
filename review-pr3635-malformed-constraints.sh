#!/bin/sh
# Standalone CLI regression; requires Go and Python 3, not the Go review tests.
# Exit 0: correct rejection; 1: confirmed bug; 2: setup/unexpected failure.
set -eu
command -v python3 >/dev/null 2>&1 || { echo 'ERROR: Python 3 required' >&2; exit 2; }
command -v go >/dev/null 2>&1 || { echo 'ERROR: Go required' >&2; exit 2; }
exec python3 - "$0" <<'PY'
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve().parent
cli = root / "bindings/go/cli"
component = "example.org/review-pr3635"
versions = ["2024.03.14", "2024.03.15", "2024.10.01"]


def interrupted(signum, frame):
    raise RuntimeError("interrupted or overall 600-second deadline exceeded")


def run(command, env, timeout=120):
    # Kill the go-run child as well as Go itself on timeout/interruption.
    with subprocess.Popen(command, cwd=cli, env=env, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, text=True, start_new_session=True) as process:
        try:
            stdout, stderr = process.communicate(timeout=timeout)
        except BaseException:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate()
            raise
        return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)


def require_success(result):
    if result.returncode:
        raise RuntimeError(f"CLI/setup failed ({result.returncode}): {result.args!r}\n"
                           f"{result.stdout}{result.stderr}")
    return result.stdout


for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGALRM):
    signal.signal(sig, interrupted)
signal.alarm(600)
try:
    # Retain Go's existing caches, but isolate CLI home/config and strip OCM overrides.
    env = {k: v for k, v in os.environ.items() if not k.startswith("OCM_")}
    caches = json.loads(require_success(run(["go", "env", "-json", "GOCACHE", "GOMODCACHE"], env)))
    with tempfile.TemporaryDirectory(prefix="review-pr3635-malformed-") as directory:
        work = Path(directory)
        home = work / "home"
        home.mkdir()
        env.update(caches, HOME=str(home), XDG_CONFIG_HOME=str(home),
                   DOCKER_CONFIG=str(home), OCM_DISABLE_VERSION_CHECK="1")
        config = work / "config.json"
        config.write_text(json.dumps({"type": "generic.config.ocm.software/v1", "configurations": [{
            "type": "versioning.config.ocm.software/v1alpha1",
            "schemes": [{"builtin": "calver-full"}],
        }]}))

        def ocm(*args):
            return run(["go", "run", "main.go", "--config", str(config), *args], env)

        repo = work / "source"
        constructor = work / "constructor.json"
        constructor.write_text(json.dumps({"components": [
            {"name": component, "version": version, "provider": {"name": "example.org"}}
            for version in versions
        ]}))
        require_success(ocm("add", "component-version", "--repository", f"ctf::{repo}",
                            "--constructor", str(constructor)))

        def query(mode, constraint):
            ref = f"ctf::{repo}//{component}"
            if mode == "get":
                return ocm("get", "component-versions", ref, "--constraint", constraint, "-o", "json")
            return ocm("transfer", "component-version", ref, f"ctf::{work / 'target'}",
                       "--constraint", constraint, "--dry-run", "-o", "json")

        def selected(mode, result):
            data = json.loads(require_success(result))
            if mode == "get":
                if not isinstance(data, list):
                    raise RuntimeError(f"Unexpected listing shape: {data!r}")
                return sorted(item["component"]["version"] for item in data)
            # Graph nodes carry versions in their IDs, references and/or typed fields.
            # Exact token boundaries avoid matching a version that is only a prefix.
            text = json.dumps(data)
            found = sorted(v for v in versions if re.search(
                r'(?<![0-9.])' + re.escape(v) + r'(?![0-9.])', text))
            if not found:
                raise RuntimeError(f"No fixture versions found in transfer graph: {text}")
            return found

        controls = [("", versions), (">=2024.03.15", versions[1:]),
                    ("<2024.10.01", versions[:2]),
                    (">=2024.03.15 <2024.10.01", [versions[1]])]
        failures = []
        for mode in ("get", "transfer"):
            for constraint, expected in controls:
                actual = selected(mode, query(mode, constraint))
                if actual != sorted(expected):
                    raise RuntimeError(f"{mode} positive control {constraint!r}: {actual} != {expected}")
                print(f"CONTROL {mode} {constraint!r}: {actual}", flush=True)
            for constraint in (">=", "<", ">=2024.03.15 <"):
                result = query(mode, constraint)
                if result.returncode:
                    error = result.stdout + result.stderr
                    # A network, build or unrelated CLI failure is not evidence of a fix.
                    if not re.search(r"(?is)(constraint.*(invalid|malformed|operand|parse|syntax)|"
                                     r"(invalid|malformed|parse|syntax).*constraint)", error):
                        raise RuntimeError(f"Unexpected rejection of {constraint!r}: {error}")
                    print(f"PASS {mode} rejects {constraint!r}: {error.strip()}", flush=True)
                    continue
                actual = selected(mode, result)
                expected_bug = versions[1:] if constraint.startswith(">=2024") else versions
                if actual != sorted(expected_bug):
                    raise RuntimeError(f"Unexpected malformed-filter result {constraint!r}: {actual}")
                failures.append(f"{mode} silently accepts {constraint!r}, selecting {actual}")
        if failures:
            for failure in failures:
                print("FAIL: " + failure, flush=True)
            sys.exit(1)
        print("PASS: all malformed constraints rejected by listing and transfer dry-run.")
except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
    print(f"ERROR (not a confirmed regression): {error}", file=sys.stderr)
    sys.exit(2)
finally:
    signal.alarm(0)
PY
