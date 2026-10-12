#!/usr/bin/env bash
# N2: the OCM hash-algorithm name -> go-digest mapping exists twice, once in the shared
# repository package and once in the s3 repository. Exit 0 = one mapping, 1 = duplicated.
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
shared="$root/bindings/go/repository/verify_digest.go"
s3="$root/bindings/go/s3/repository/resource_repository.go"
grep -n 'func hashAlgorithm' "$shared" || exit 2
if grep -n 'func requestedAlgorithm' "$s3"; then
  echo "duplicated: both map \"sha256\"/\"sha512\" names to digest algorithms"
  grep -n 'case "sha256"\|case "", "sha256"\|case "sha512"' "$shared" "$s3"
  exit 1
fi
