#!/bin/sh
# PR #3635: a configured versioning scheme is honoured on write and ignored on read.
# Run from the repository root.
set -e

cd bindings/go/cli
go build -o /tmp/ocm-repro-cli .

rm -rf /tmp/ocm-repro
mkdir /tmp/ocm-repro
cd /tmp/ocm-repro

cat > versioning.ocmconfig <<'EOF'
type: generic.config.ocm.software/v1
configurations:
  - type: versioning.config.ocm.software/v1alpha1
    schemes:
      - name: calver-build
        pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})\.(?P<build>\d+)$'
        comparisonGroups: [year, month, day, build]
EOF

cat > cc-a.yaml <<'EOF'
components:
  - name: acme.org/svc
    version: 2024.03.15
    provider:
      name: acme
EOF

cat > cc-b.yaml <<'EOF'
components:
  - name: acme.org/svc
    version: 2024.03.15.7
    provider:
      name: acme
EOF

echo "=== add cv: both succeed ==="
/tmp/ocm-repro-cli --config versioning.ocmconfig add cv --repository ./ctf --constructor cc-a.yaml
/tmp/ocm-repro-cli --config versioning.ocmconfig add cv --repository ./ctf --constructor cc-b.yaml

echo
echo "=== tags actually written to the CTF ==="
grep -o '"tag":"[^"]*"' ctf/artifact-index.json

echo
echo "=== get cv: expect both, 2024.03.15.7 is missing ==="
/tmp/ocm-repro-cli --config versioning.ocmconfig get cv ./ctf//acme.org/svc

echo
echo "=== get cv --latest: expect 2024.03.15.7, reports 2024.03.15 ==="
/tmp/ocm-repro-cli --config versioning.ocmconfig get cv --latest ./ctf//acme.org/svc
