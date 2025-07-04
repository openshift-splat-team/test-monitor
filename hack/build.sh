#!/bin/sh

set -ex

# shellcheck disable=SC2068
version() { IFS="."; printf "%03d%03d%03d\\n" $@; unset IFS;}

minimum_go_version=1.14
current_go_version=$(go version | cut -d " " -f 3)

if [ "$(version "${current_go_version#go}")" -lt "$(version "$minimum_go_version")" ]; then
		echo "Go version should be greater or equal to $minimum_go_version"
		exit 1
fi

MODE="${MODE:-release}"
GIT_COMMIT="${SOURCE_GIT_COMMIT:-$(git rev-parse --verify 'HEAD^{commit}')}"
GIT_TAG="${BUILD_VERSION:-$(git describe --always --abbrev=40 --dirty)}"
GOFLAGS="${GOFLAGS:--mod=vendor}"
LDFLAGS="${LDFLAGS} -X github.com/openshift-splat-team/test-monitor/pkg/version.Raw=${GIT_TAG} -X github.com/openshift-splat-team/test-monitor/pkg/version.Commit=${GIT_COMMIT}"
TAGS="${TAGS:-}"
OUTPUT="${OUTPUT:-bin/test-monitor}"
export CGO_ENABLED=0

case "${MODE}" in
release)
	LDFLAGS="${LDFLAGS} -s -w"
	TAGS="${TAGS} release okd"
	;;
dev)
	;;
*)
	echo "unrecognized mode: ${MODE}" >&2
	exit 1
esac

if (echo "${TAGS}" | grep -q 'libvirt')
then
	export CGO_ENABLED=1
fi

go build "${GOFLAGS}" -ldflags "${LDFLAGS}" -tags "${TAGS}" -o "${OUTPUT}" ./cmd/test-monitor
