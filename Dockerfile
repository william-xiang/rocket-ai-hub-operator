# Build the manager binary
FROM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/controller/ internal/controller/
COPY kubeflow-ppc64le-manifests/ manifests/
COPY gpu-operator/deployments/gpu-operator/ gpu-operator/
COPY keycloak/ keycloak/
COPY version/ version/
COPY pkg/ pkg/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Use alpine as minimal base image to package the manager binary
FROM alpine:latest
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/manifests/ manifests/
COPY --from=builder /workspace/gpu-operator/ gpu-operator/
COPY --from=builder /workspace/keycloak/ keycloak/

# Install git which is used by Krusty to load manifests from a Git URL
RUN apk update && apk add git

# Change the permission of files
RUN chmod -R 777 manifests/ keycloak/

USER 65532:65532

ENTRYPOINT ["/manager"]
