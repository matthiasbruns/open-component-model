#!/bin/sh
# Standalone CLI regression; requires Go and Python 3, not the Go review tests.
# Exit 0: correct mixed filtering; 1: confirmed bug; 2: setup/unexpected failure.
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
cli = root / "bindings/go/cli"
component = "example.org/review-pr3635"


def interrupted(signum, frame):
    raise RuntimeError("interrupted or overall 600-second deadline exceeded")


def run(command, env, timeout=120):
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
    env = {k: v for k, v in os.environ.items() if not k.startswith("OCM_")}
    caches = json.loads(require_success(run(["go", "env", "-json", "GOCACHE", "GOMODCACHE"], env)))
    with tempfile.TemporaryDirectory(prefix="review-pr3635-mixed-") as directory:
        work = Path(directory)
        home = work / "home"
        home.mkdir()
        env.update(caches, HOME=str(home), XDG_CONFIG_HOME=str(home),
                   DOCKER_CONFIG=str(home), OCM_DISABLE_VERSION_CHECK="1")
        config = work / "config.json"
        config.write_text(json.dumps({"type": "generic.config.ocm.software/v1", "configurations": [{
            "type": "versioning.config.ocm.software/v1alpha1",
            "schemes": [{"name": "build", "pattern": r"^build-(?P<n>\d+)$",
                         "comparisonGroups": ["n"]}, {"builtin": "loose-semver"}],
        }]}))

        def ocm(*args):
            return run(["go", "run", "main.go", "--config", str(config), *args], env)

        repo = work / "source"

        def add(versions):
            constructor = work / "constructor.json"
            constructor.write_text(json.dumps({"components": [
                {"name": component, "version": version, "provider": {"name": "example.org"}}
                for version in versions
            ]}))
            require_success(ocm("add", "component-version", "--repository", f"ctf::{repo}",
                                "--constructor", str(constructor)))

        def query(constraint):
            return ocm("get", "component-versions", f"ctf::{repo}//{component}",
                       "--constraint", constraint, "-o", "json")

        def check(result, expected, label):
            data = json.loads(require_success(result))
            if not isinstance(data, list):
                raise RuntimeError(f"Unexpected listing shape: {data!r}")
            actual = sorted(item["component"]["version"] for item in data)
            if actual != sorted(expected):
                raise RuntimeError(f"{label}: {actual} != {sorted(expected)}")
            print(f"CONTROL {label}: {actual}", flush=True)

        # The same registry and constraint work before semver history is added.
        add(["build-99", "build-100", "build-101"])
        check(query(""), ["build-99", "build-100", "build-101"], "custom-only round-trip")
        check(query(">=build-100"), ["build-100", "build-101"], "custom-only >=build-100")
        check(query("<build-100"), ["build-99"], "custom-only <build-100")
        add(["1.0.0", "2.0.0"])
        check(query(""), ["build-99", "build-100", "build-101", "1.0.0", "2.0.0"],
              "mixed history round-trip")
        check(query(">=2.0.0"), ["build-99", "build-100", "build-101", "2.0.0"],
              "mixed semver >=2.0.0 retains custom versions")
        check(query("<2.0.0"), ["build-99", "build-100", "build-101", "1.0.0"],
              "mixed semver <2.0.0 retains custom versions")
        result = query(">=build-100")
        if result.returncode:
            error = result.stdout + result.stderr
            if ('filtering component versions failed' in error
                    and 'parsing semantic version constraint failed: improper constraint: ">=build-100"' in error):
                print("FAIL: adding semver history breaks the previously valid custom filter:\n"
                      + error.strip(), flush=True)
                sys.exit(1)
            raise RuntimeError(f"Unexpected mixed-filter failure: {error}")
        check(result, ["build-100", "build-101", "1.0.0", "2.0.0"],
              "mixed custom >=build-100 retains semver versions")
        print("PASS: custom and semver constraints both work on mixed history.")
except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
    print(f"ERROR (not a confirmed regression): {error}", file=sys.stderr)
    sys.exit(2)
finally:
    signal.alarm(0)
PY
