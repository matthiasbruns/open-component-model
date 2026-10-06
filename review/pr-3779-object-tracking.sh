#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root/bindings/go"
go test ./kubernetes/controller/internal/controller/deployer \
  -ginkgo.focus='Review evidence: NamespacedDeployer object tracking' -count=1
