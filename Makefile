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
GOLANGCI   ?= $(OUT)/golangci-lint
CMDS       := $(notdir $(wildcard cmd/*))
SUDO       ?= $(shell [ "$$(id -u)" = 0 ] || echo sudo)
IMAGE      ?= tbnet
DOCKER_TTY := $(shell [ -t 0 ] && echo -it)
CMD        ?= bash
VM         ?= tbnet
VM_SOCK    := $(HOME)/.lima/$(VM)/sock/docker.sock

# docker-* targets use the Lima VM's daemon when it is up (macOS), else the default one (CI)
ifneq ($(wildcard $(VM_SOCK)),)
export DOCKER_HOST ?= unix://$(VM_SOCK)
endif

.PHONY: all build test lint lint-fix fmt bench plot env-local-up env-local-down docker-build docker-test docker-bench dev vm-up vm-down clean

all: build

build: $(CMDS:%=$(OUT)/%)

$(OUT)/%: cmd/% $(shell find . -name '*.go' 2>/dev/null)
	@mkdir -p $(OUT)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -o $@ ./cmd/$*

test:
	$(GO) test -v -race ./...

lint: $(GOLANGCI)
	$(GO) vet ./...
	$(GOLANGCI) run

lint-fix: $(GOLANGCI)
	$(GOLANGCI) run --fix

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
	docker build --build-arg GOLANGCI_VERSION=$(GOLANGCI_VERSION) -t $(IMAGE) .

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

# macOS: Lima VM with a stock kernel (userfaultfd, KVM) running the Docker daemon
vm-up:
	@if limactl list -q 2>/dev/null | grep -qx $(VM); then limactl start -y $(VM); \
	else limactl start -y --name=$(VM) --mount-only=$(CURDIR):w env/local/lima.yaml; fi

vm-down:
	limactl stop $(VM)

clean:
	rm -rf out
