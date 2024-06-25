# Build node feature discovery
FROM registry.access.redhat.com/ubi9/go-toolset:1.20.10-6 as builder


WORKDIR /go/node-feature-discovery
COPY . .

# Do actual build
ARG VERSION=v0.10.0
ARG HOSTMOUNT_PREFIX=/host-
RUN make build VERSION=${VERSION} HOSTMOUNT_PREFIX=${HOSTMOUNT_PREFIX}

# Create full variant of the production image
FROM registry.access.redhat.com/ubi9/ubi-minimal:9.4

# Use more verbose logging of gRPC
ENV GRPC_GO_LOG_SEVERITY_LEVEL="INFO"

COPY --from=builder /go/node-feature-discovery/deployment/components/worker-config/nfd-worker.conf.example /etc/kubernetes/node-feature-discovery/nfd-worker.conf
COPY --from=builder /go/node-feature-discovery/bin/* /usr/bin/
