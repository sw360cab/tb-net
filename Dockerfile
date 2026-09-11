# Linux build + test image, so everything runs the same on a Mac.
# Tests need netns, which needs --privileged at run time (not available during
# docker build), so the test stage only sets the command: make docker-test.

FROM golang:1.24.5-bookworm AS base
RUN apt-get update \
 && apt-get install -y --no-install-recommends iproute2 iputils-ping \
 && rm -rf /var/lib/apt/lists/*
# bind-mounted repo is owned by the host user; let root's git read it
RUN git config --system --add safe.directory /src
WORKDIR /src

# compile: static binaries into out/, vet, and prebuild race test binaries
# into the build cache so the test run doesn't recompile
FROM base AS build
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN make build \
 && go vet ./... \
 && go test -race -run '^$' ./...

# test: bring up the local env, run the suite, tear it down
FROM build AS test
CMD ["make", "env-local-up", "test", "env-local-down"]
