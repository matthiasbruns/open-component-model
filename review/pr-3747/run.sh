#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
printf 'Reviewed PR head: 35a8fd76eb4f49e1156659a9d6a8b8a47a38da76\n'
go version
python3 review/pr-3747/source_audit.py
cd bindings/go
GOFIPS140=certified GODEBUG=fips140=on go test -count=1 -v ./sigstore/signing/handler/internal -run 'TestReviewFIPSEvidenceRealBinary|TestResolveBinary_CosignOnPathFIPSBuild|TestResolveBinary_NoCosignOnPath'
GOFIPS140=certified GODEBUG=fips140=on go test -count=1 -v ./gpg/signing/handler/internal/gpgbinary -run TestBinary_Resolve_FIPSMode
for mode in on only; do
  printf '\nDigest checks in %s mode\n' "$mode"
  GOFIPS140=certified GODEBUG=fips140="$mode" go test -count=1 -v ./signing -run 'TestReviewFIPSEvidenceDigestRuntimeMode|TestValidateDigestHashAlgorithms'
done
