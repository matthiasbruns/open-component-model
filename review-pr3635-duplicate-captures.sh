#!/bin/sh
# PR 3635 finding 3; standalone CLI repro, requiring only Go and Python 3.
# Exit 0: correct ordering or explicit duplicate-name rejection; 1: bug; 2: setup failure.
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
cli_dir = root / "bindings/go/cli"
env = dict(os.environ, OCM_DISABLE_VERSION_CHECK="1")
component = "example.org/review-pr3635"


def interrupted(signum, frame):
    raise RuntimeError("interrupted or overall 300-second deadline exceeded")


def run(config, *args):
    command = ["go", "run", "main.go", "--config", str(config), *args]
    with subprocess.Popen(command, cwd=cli_dir, env=env, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, text=True, start_new_session=True) as process:
        try:
            stdout, stderr = process.communicate(timeout=120)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
    return process.returncode, stdout, stderr


def checked(config, *args):
    code, stdout, stderr = run(config, *args)
    if code:
        raise RuntimeError(f"CLI failed ({code}): {args!r}\n{stdout}{stderr}")
    return stdout


def versions(config, repo, constraint):
    raw = checked(config, "get", "component-versions", f"ctf::{repo}//{component}",
                  "--constraint", constraint, "--output", "json")
    data = json.loads(raw)
    if not isinstance(data, list):
        raise RuntimeError(f"Unexpected listing: {raw}")
    return [item["component"]["version"] for item in data]


for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGALRM):
    signal.signal(sig, interrupted)
signal.alarm(300)
try:
    with tempfile.TemporaryDirectory(prefix="review-pr3635-duplicates-") as directory:
        work = Path(directory)
        config = work / "config.json"
        constructor = work / "constructor.json"
        constructor.write_text(json.dumps({"components": [
            {"name": component, "version": version, "provider": {"name": "example.org"}}
            for version in ("v2", "v10")
        ]}))

        def configure(pattern):
            config.write_text(json.dumps({
                "type": "generic.config.ocm.software/v1",
                "configurations": [{
                    "type": "versioning.config.ocm.software/v1alpha1",
                    "schemes": [{"name": "alternative-prefixes", "pattern": pattern,
                                 "comparisonGroups": ["n"]}],
                }],
            }))

        # A valid, equivalent single-capture control distinguishes setup failures.
        configure(r"^(?:v|r)(?P<n>\d+)$")
        control = work / "control"
        checked(config, "add", "component-version", "--repository", f"ctf::{control}",
                "--constructor", str(constructor))
        if versions(config, control, ">=v10") != ["v10"]:
            raise RuntimeError("single-capture control did not select only v10")

        configure(r"^(?:v(?P<n>\d+)|r(?P<n>\d+))$")
        repo = work / "duplicates"
        code, stdout, stderr = run(config, "add", "component-version", "--repository",
                                   f"ctf::{repo}", "--constructor", str(constructor))
        if code:
            diagnostic = stdout + stderr
            # Only a config diagnostic about duplicate captures counts as a fix.
            rejection = any(
                "alternative-prefixes" in line and
                re.search(r"duplicate|repeated|unique", line, re.I) and
                re.search(r"capture|group|name", line, re.I)
                for line in diagnostic.splitlines()
            )
            if not rejection:
                raise RuntimeError(f"Unrelated CLI failure ({code}):\n{diagnostic}")
            print("PASS: duplicate capture configuration explicitly rejected:\n" + diagnostic)
            sys.exit(0)
        all_versions = versions(config, repo, "")
        if len(all_versions) != 2 or set(all_versions) != {"v2", "v10"}:
            raise RuntimeError(f"Fixture did not round-trip: {all_versions!r}")
        selected = versions(config, repo, ">=v10")
        print(f"duplicate captures: all={all_versions!r}; >=v10={selected!r}", flush=True)
        if len(selected) != len(set(selected)) or not set(selected) <= {"v2", "v10"} or "v10" not in selected:
            raise RuntimeError(f"Unexpected constrained listing: {selected!r}")
        if "v2" in selected:
            print("FAIL: v2 incorrectly satisfies >=v10 with duplicate capture name n")
            sys.exit(1)
        print("PASS: >=v10 selects only v10")
except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
    print(f"ERROR (not a confirmed regression): {error}", file=sys.stderr)
    sys.exit(2)
finally:
    signal.alarm(0)
PY
