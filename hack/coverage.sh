#!/usr/bin/env bash
set -e

source hack/common.sh
source hack/bootstrap.sh

if [ "${CI}" == "true" ]; then
    cat >>ci.bazelrc <<EOF
coverage --cache_test_results=no --runs_per_test=1
EOF
fi

# TODO: rules_go now supports full bazel lcov integration.
# Let's move over to that, since the manual coverage merge step is then not needed anymore.
bazel coverage \
    --config=${ARCHITECTURE} \
    --@io_bazel_rules_go//go/config:cover_format=go_cover \
    --@io_bazel_rules_go//go/config:race \
    --test_output=errors -- //staging/src/kubevirt.io/client-go/... //pkg/... //cmd/... -//cmd/virtctl/...
