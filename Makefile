.PHONY: all build test test-update lint vendor clean

BIN_DIR := bin
BINARY := $(BIN_DIR)/harvester-cmdline

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

clean:
	rm -rf $(BIN_DIR)
