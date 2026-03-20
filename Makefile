.PHONY: build build-dir build-hnet build-hnetd install uninstall test vet fmt tidy verify clean

GO ?= go
BIN_DIR ?= $(HOME)/.local/bin
BUILD_DIR ?= ./build
INSTALL ?= install
GOFLAGS ?=

HNET_BIN := $(BUILD_DIR)/hnet
HNETD_BIN := $(BUILD_DIR)/hnetd

build: build-dir build-hnet build-hnetd

build-dir:
	mkdir -p "$(BUILD_DIR)"

build-hnet:
	$(GO) build $(GOFLAGS) -o "$(HNET_BIN)" ./cmd/hnet

build-hnetd:
	$(GO) build $(GOFLAGS) -o "$(HNETD_BIN)" ./cmd/hnetd

install: build
	mkdir -p "$(BIN_DIR)"
	$(INSTALL) -m 0755 "$(HNET_BIN)" "$(BIN_DIR)/hnet"
	$(INSTALL) -m 0755 "$(HNETD_BIN)" "$(BIN_DIR)/hnetd"

uninstall:
	rm -f "$(BIN_DIR)/hnet" "$(BIN_DIR)/hnetd"

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w ./cmd ./internal

tidy:
	$(GO) mod tidy

verify: fmt test build

clean:
	rm -rf "$(BUILD_DIR)"
	rm -f ./hnet ./hnetd
