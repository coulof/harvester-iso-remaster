.PHONY: all build test test-update lint vendor clean container-build container-test

BIN_DIR := bin
BINARY := $(BIN_DIR)/harvester-cmdline
CONTAINER_CLI ?= container
IMAGE_NAME ?= harvester-iso-remaster:local

all: build test

build:
	mkdir -p $(BIN_DIR)
	go build -mod=vendor -o $(BINARY) ./cmd/harvester-cmdline

test:
	go test -mod=vendor -v ./...

test-update:
	go test -mod=vendor -v -run TestGoldenFiles -update ./...

lint:
	go vet -mod=vendor ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "Running golangci-lint..."; \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping golangci-lint"; \
	fi

vendor:
	go mod tidy
	go mod vendor

container-build:
	$(CONTAINER_CLI) build -t $(IMAGE_NAME) .

container-test: container-build
	@echo "[+] Testing container entrypoint (--help)..."
	$(CONTAINER_CLI) run --rm $(IMAGE_NAME) --help
	@echo "[+] Testing harvester-cmdline inside container..."
	$(CONTAINER_CLI) run --rm --entrypoint harvester-cmdline $(IMAGE_NAME) --version
	@echo "[✓] Container smoke tests passed."

clean:
	rm -rf $(BIN_DIR)
