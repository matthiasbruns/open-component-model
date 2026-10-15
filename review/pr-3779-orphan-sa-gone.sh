#!/bin/sh
# Runs the review specs for PR 3779 (Orphan policy without service account, DeletionFailed attribution).
# Needs envtest assets: KUBEBUILDER_ASSETS=$(setup-envtest use -p path) or the controller Taskfile's bin/k8s/<version>.
set -eu
repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root/bindings/go/kubernetes/controller"
go test ./internal/controller/deployer/ -count=1 -ginkgo.v \
  -ginkgo.focus='Review evidence: Orphan deletion policy without service account'
