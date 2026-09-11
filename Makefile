# farmem — see CLAUDE.md §4
GO         ?= go
GOFLAGS    ?=
PLATFORM   := $(shell $(GO) env GOOS)_$(shell $(GO) env GOARCH)
OUT        ?= out/$(PLATFORM)
ENV        ?= local
LINK       ?= tcp
MODE       ?= rtt
CGO_ENABLED?= 0
GOLANGCI_VERSION ?= v2.13.2
CMDS       := $(notdir $(wildcard cmd/*))
SUDO       ?= $(shell [ "$$(id -u)" = 0 ] || echo sudo)
IMAGE      ?= tbnet
DOCKER_TTY := $(shell [ -t 0 ] && echo -it)
CMD        ?= bash

.PHONY: all build test lint lint-fix fmt bench plot env-local-up env-local-down docker-build docker-test docker-bench dev clean

all: build

build: $(CMDS:%=$(OUT)/%)

$(OUT)/%: cmd/% $(shell find . -name '*.go' 2>/dev/null)
	@mkdir -p $(OUT)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -o $@ ./cmd/$*

test:
	$(GO) test -v -race ./...

lint: $(OUT)/golangci-lint
	$(GO) vet ./...
	$(OUT)/golangci-lint run

lint-fix: $(OUT)/golangci-lint
	$(OUT)/golangci-lint run --fix

# pinned, installed outside the module so go.mod stays dependency-free
$(OUT)/golangci-lint:
	@mkdir -p $(OUT)
	GOBIN=$(abspath $(OUT)) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

fmt:
	$(GO) fmt ./...

bench: build
	$(OUT)/farbench -env $(ENV) -link $(LINK) -mode $(MODE) -out bench/results/

plot:
	python3 bench/plot.py

env-local-up:
	$(SUDO) env/local/up.sh

env-local-down:
	$(SUDO) env/local/down.sh

docker-build:
	docker build -t $(IMAGE) .

# compile, bring up env/local, run tests: all inside Linux
docker-test: docker-build
	docker run --rm --privileged $(IMAGE)

# benchmarks run on the live tree: .git for farbench's SHA and dirty check,
# and bench/results/ lands on the host
docker-bench: docker-build
	docker run --rm --privileged -v $(CURDIR):/src $(IMAGE) \
		make env-local-up bench ENV=$(ENV) LINK=$(LINK) MODE=$(MODE)

# Linux root shell on the live source tree, e.g. make dev CMD="make lint"
dev: docker-build
	docker run --rm $(DOCKER_TTY) --privileged -v $(CURDIR):/src $(IMAGE) $(CMD)

clean:
	rm -rf out
