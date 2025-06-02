#!/bin/sh

if [ "$IS_CONTAINER" != "" ]; then
  set -xe
  go generate ./pkg/apis/test-monitor.splat-team.io/install.go
  set +ex
  # git diff --exit-code
else
  podman run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/test-monitor:z" \
    --workdir /go/src/github.com/openshift/test-monitor \
    docker.io/golang:1.18 \
    ./hack/verify-codegen.sh "${@}"
fi
