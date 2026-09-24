#!/bin/sh
# CLI-only regression for PR 3635: mixed numeric/text capture ordering.
# Run: sh review-pr3635-cli-ordering.sh
# Requires Go and Python 3; no Go review tests or additional Go dependencies.
# Exit 1: confirmed ordering bug; 0: consistent results; 2: setup/CLI failure.
set -eu
exec python3 - "$0" <<'PY'
import itertools
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve().parent
cli_dir = root / "bindings/go/cli"
component = "example.org/review-pr3635"
versions = ("2", "10", "1a")
env = dict(os.environ, OCM_DISABLE_VERSION_CHECK="1")


def interrupted(signum, frame):
    raise RuntimeError("interrupted or overall 300-second deadline exceeded")


signal.signal(signal.SIGTERM, interrupted)
signal.signal(signal.SIGALRM, interrupted)
signal.alarm(300)

try:
    with tempfile.TemporaryDirectory(prefix="review-pr3635-cli-") as directory:
        work = Path(directory)
        config = work / "config.json"
        config.write_text(json.dumps({
            "type": "generic.config.ocm.software/v1",
            "configurations": [{
                "type": "versioning.config.ocm.software/v1alpha1",
                "schemes": [{
                    "name": "mixed-capture",
                    "pattern": "^(?P<value>[0-9a-z]+)$",
                    "comparisonGroups": ["value"],
                }],
            }],
        }))

        def ocm(*args):
            command = ["go", "run", "main.go", "--config", str(config), *args]
            result = subprocess.run(command, cwd=cli_dir, env=env,
                                    capture_output=True, text=True, timeout=120)
            if result.returncode:
                raise RuntimeError(f"CLI failed ({result.returncode}): {command!r}\n"
                                   f"{result.stdout}{result.stderr}")
            return result.stdout

        def get_versions(repo, latest=False):
            args = ["get", "component-versions", f"ctf::{repo}//{component}",
                    "--constraint", "", "--output", "json"]
            if latest:
                args.append("--latest")
            raw = ocm(*args)
            descriptors = json.loads(raw)
            if not isinstance(descriptors, list):
                raise RuntimeError(f"Unexpected JSON listing shape: {raw}")
            return [descriptor["component"]["version"] for descriptor in descriptors]

        def fixture(order):
            name = "-".join(order)
            repo = work / ("ctf-" + name)
            constructor = work / (name + ".json")
            constructor.write_text(json.dumps({"components": [
                {"name": component, "version": version,
                 "provider": {"name": "example.org"}}
                for version in order
            ]}))
            # Never construct the CTF's index or filesystem blobs by hand.
            ocm("add", "component-version", "--repository", f"ctf::{repo}",
                "--constructor", str(constructor))
            listing = get_versions(repo)
            latest = get_versions(repo, latest=True)
            if len(listing) != len(order) or set(listing) != set(order):
                raise RuntimeError(f"Fixture {order!r} did not round-trip: {listing!r}")
            if len(latest) != 1 or latest[0] not in order:
                raise RuntimeError(f"Unexpected latest result: {latest!r}")
            print(f"insert={','.join(order)} listing={','.join(listing)} "
                  f"latest={latest[0]}", flush=True)
            return listing, latest[0]

        # Derive the pairwise relation from the real CLI, not a copied comparator.
        pair_orders = {}
        pair_winners = {}
        for pair in itertools.combinations(versions, 2):
            listing, latest = fixture(pair)
            key = frozenset(pair)
            pair_orders[key] = listing
            pair_winners[key] = latest

        failures = []
        newest = set()
        for order in itertools.permutations(versions):
            listing, latest = fixture(order)
            newest.add(latest)
            for pair, expected in pair_orders.items():
                actual = [version for version in listing if version in pair]
                if actual != expected:
                    failures.append(f"insert={','.join(order)}: listing pair {actual} "
                                    f"contradicts two-version CLI order {expected}")
            for other in versions:
                if other != latest and pair_winners[frozenset((latest, other))] != latest:
                    failures.append(f"insert={','.join(order)}: --latest={latest}, "
                                    f"but pairwise --latest({latest},{other})={other}")
        if len(newest) > 1:
            failures.append("--latest depends on insertion order: " + ",".join(sorted(newest)))
        if failures:
            print("FAIL: CLI ordering is not globally consistent:", flush=True)
            for failure in failures:
                print("  " + failure, flush=True)
            sys.exit(1)
        print("PASS: all six insertion permutations agree with pairwise CLI ordering.")
except (OSError, ValueError, KeyError, TypeError, RuntimeError,
        subprocess.SubprocessError) as error:
    print(f"ERROR (not a confirmed regression): {error}", file=sys.stderr)
    sys.exit(2)
finally:
    signal.alarm(0)
PY
