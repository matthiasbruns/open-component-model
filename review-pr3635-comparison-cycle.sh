#!/bin/sh
# Standalone public-API regression; run with sh from any working directory.
# Requires Go and Python 3. Exit 0: pass; 1: ordering bug; 2: setup failure.
set -eu
exec python3 - "$0" <<'PY'
import itertools
import json
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve().parent
source = r'''
package main

import (
    "encoding/json"
    "fmt"
    "regexp"

    "ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func main() {
    registry := versioning.NewRegistry(versioning.NewRegexScheme(
        "mixed-capture", regexp.MustCompile(`^(?P<value>[0-9a-z]+)$`), []string{"value"}))
    versions := []string{"2", "10", "1a", "a", "b"}
    comparisons := map[string]map[string]int{}
    for _, a := range versions {
        comparisons[a] = map[string]int{}
        for _, b := range versions {
            comparison, err := registry.Compare(a, b)
            if err != nil { panic(err) }
            comparisons[a][b] = comparison
        }
    }
    permutations := [][]string{
        {"2", "10", "1a"}, {"2", "1a", "10"},
        {"10", "2", "1a"}, {"10", "1a", "2"},
        {"1a", "2", "10"}, {"1a", "10", "2"},
    }
    for _, versions := range permutations {
        if err := registry.SortDescending(versions); err != nil { panic(err) }
    }
    data, err := json.Marshal(struct {
        Comparisons map[string]map[string]int
        Sorted [][]string
    }{comparisons, permutations})
    if err != nil { panic(err) }
    fmt.Println(string(data))
}
'''
try:
    with tempfile.TemporaryDirectory(prefix="review-pr3635-comparison-") as directory:
        main = Path(directory) / "main.go"
        main.write_text(source)
        result = subprocess.run(["go", "run", str(main)], cwd=root / "bindings/go",
                                capture_output=True, text=True, timeout=120)
        if result.returncode:
            raise RuntimeError(result.stdout + result.stderr)
        data = json.loads(result.stdout)
    comparisons = data["Comparisons"]
    if comparisons["2"]["10"] >= 0 or comparisons["a"]["b"] >= 0:
        raise RuntimeError("Numeric/text control comparisons failed")
    print("PASS controls: numeric 2 < 10; lexical a < b")
    failures = []
    for a, b, c in itertools.permutations(("2", "10", "1a")):
        if comparisons[a][b] < 0 and comparisons[b][c] < 0 and comparisons[a][c] >= 0:
            failures.append(f"{a} < {b} and {b} < {c}, but Compare({a}, {c})={comparisons[a][c]}")
    for original, ordered in zip(itertools.permutations(("2", "10", "1a")), data["Sorted"]):
        print(f"SortDescending({list(original)}) = {ordered}")
        for i, a in enumerate(ordered):
            for b in ordered[i + 1:]:
                if comparisons[a][b] < 0:
                    failures.append(f"descending output {ordered} places smaller {a} before {b}")
    winners = {ordered[0] for ordered in data["Sorted"]}
    if len(winners) > 1:
        failures.append(f"same version set has different newest values: {sorted(winners)}")
    if failures:
        print("FAIL: comparison/sorting violates a consistent ordering:")
        for failure in failures:
            print("  " + failure)
        sys.exit(1)
    print("PASS: transitivity and all six sorted permutations are consistent.")
except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
    print(f"ERROR (not a confirmed regression): {error}", file=sys.stderr)
    sys.exit(2)
PY
