#!/usr/bin/env python3
"""Run checksum evidence on head, merge-base, or a temporary safeguard overlay."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(__file__).resolve().parents[2]
mode = sys.argv[1] if len(sys.argv) > 1 else "head"
if mode not in ("head", "base", "restored"):
    raise SystemExit("usage: check_encoding.py [head|base|restored]")


def git(*args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


head = git("rev-parse", "HEAD")
base = git("merge-base", head, "d090da3ad87ba2f95f687cfb906e454fb465f2ec")
print(f"mode={mode} head={head} merge-base={base}", flush=True)
prefix = "bindings/go/wget/"
with tempfile.TemporaryDirectory(prefix="pr3939-encoding-") as scratch:
    scratch = Path(scratch)
    replacements = {}
    if mode == "base":
        names = git("ls-tree", "-r", "--name-only", base, "--", prefix).splitlines()
        for name in names:
            if not name.endswith(".go"):
                continue
            dest = scratch / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(subprocess.check_output(["git", "-C", str(root), "show", f"{base}:{name}"]))
            replacements[str(root / name)] = str(dest)
        for name in git("ls-files", prefix).splitlines():
            if name.endswith(".go") and name not in names and not Path(name).name.startswith("review_pr3939"):
                replacements[str(root / name)] = ""
        # The baseline constructor takes a filesystem config before its options.
        name = prefix + "repository/review_pr3939_encoding_test.go"
        dest = scratch / "public_test.go"
        dest.write_text((root / name).read_text().replace(
            "repository.NewResourceRepository(opts...)",
            "repository.NewResourceRepository(nil, opts...)",
        ))
        replacements[str(root / name)] = str(dest)
    elif mode == "restored":
        name = prefix + "internal/download/download.go"
        source = (root / name).read_text()
        marker = "\tif req.NoRedirect {"
        assert source.count(marker) == 1
        source = source.replace(marker,
            '\tif len(o.DigestAlgorithms) > 0 {\n'
            '\t\thttpReq.Header.Set("Accept-Encoding", "identity")\n'
            '\t}\n' + marker)
        dest = scratch / "download.go"
        dest.write_text(source)
        replacements[str(root / name)] = str(dest)
    command = ["go", "test"]
    if replacements:
        overlay = scratch / "overlay.json"
        overlay.write_text(json.dumps({"Replace": replacements}))
        command.append(f"-overlay={overlay}")
    command.extend([
        "./wget/repository", "./wget/internal/download",
        "-run", "TestReviewPR3939_(ValidHTTPChecksumWithStandardClient|ChecksumBytesMustNotBeTransparentlyDecoded)$",
        "-count=1", "-v",
    ])
    print(" ".join(command), flush=True)
    result = subprocess.run(command, cwd=root / "bindings/go")
    raise SystemExit(result.returncode)
