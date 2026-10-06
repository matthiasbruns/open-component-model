#!/bin/sh
set -eu
cd "$(dirname "$0")/../../bindings/go"
rtk proxy go test ./s3/repository -run '^TestReview_' -count=1
