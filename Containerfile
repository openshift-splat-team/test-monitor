FROM registry.ci.openshift.org/openshift/release:golang-1.23 AS builder
WORKDIR /go/src/github.com/openshift-splat-team/test-monitor
COPY . .
ENV GO_PACKAGE github.com/openshift-splat-team/test-monitor
RUN NO_DOCKER=1 make build

# FROM registry.ci.openshift.org/openshift/origin-v4.0:base
# FROM registry.ci.openshift.org/ocp/4.13:base
FROM registry.redhat.io/ubi9-minimal@sha256:92b1d5747a93608b6adb64dfd54515c3c5a360802db4706765ff3d8470df6290
COPY --from=builder /go/src/github.com/openshift-splat-team/test-monitor/bin/test-monitor /usr/bin/test-monitor
ENTRYPOINT ["/usr/bin/test-monitor"]
LABEL io.openshift.release.operator=true