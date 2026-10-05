"""Source observations, not runtime tests or a certification assessment."""
from pathlib import Path

root = Path(__file__).resolve().parents[2]
env = (root / ".env").read_text()
cli = (root / "bindings/go/cli/cmd/setup/hooks/pre_run.go").read_text()
docs = (root / "website/content/docs/reference/standards-and-regulations/fips.md").read_text()
ci = (root / ".github/workflows/ci.yml").read_text()
assert "GOFIPS140=certified" in env
assert 'slog.DebugContext(cmd.Context(), "FIPS 140-3 mode"' in cli
assert "| Signing and verification | Resource and component reference digests must use SHA-256 or SHA-512 | Same |" in docs
assert "| `ocm sign cv` |" in docs and "Signs, logs a warning" in docs
assert "run: task bindings/go:test" in ci and "fips140=only" not in ci
print("SOURCE: module selector is certified, not an explicit version")
print("SOURCE: CLI boot-time FIPS status is DEBUG")
print("SOURCE: opening digest table says on requires SHA-256/SHA-512; later table says on warns")
print("SOURCE: CI has no fips140=only run")
