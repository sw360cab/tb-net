# Linux build + test image, so everything runs the same on a Mac.
# Tests need netns, which needs --privileged at run time (not available during
# docker build), so the test stage only sets the command: make docker-test.
# On macOS the daemon is the Lima VM's (make vm-up): Docker Desktop's kernel
# has no userfaultfd.

FROM golang:1.26.8-bookworm AS base
RUN apt-get update \
 && apt-get install -y --no-install-recommends iproute2 iputils-ping python3-matplotlib \
 && rm -rf /var/lib/apt/lists/*
# bind-mounted repo is owned by the host user; let root's git read it
RUN git config --system --add safe.directory /src
# pinned linter; make docker-build passes GOLANGCI_VERSION from the Makefile
ARG GOLANGCI_VERSION=v2.13.2
RUN GOBIN=/usr/local/bin \
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION} \
 && go clean -cache -modcache
ENV GOLANGCI=/usr/local/bin/golangci-lint
WORKDIR /src

# compile: static binaries into out/, vet + lint, and prebuild race test binaries
# into the build cache so the test run doesn't recompile
FROM base AS build
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN make build lint \
 && go test -race -run '^$' ./...

# test: bring up the local env, run the suite, tear it down
FROM build AS test
CMD ["make", "env-local-up", "test", "env-local-down"]
