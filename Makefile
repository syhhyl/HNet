.PHONY: build build-hnet build-hnetd install uninstall test vet fmt tidy verify clean

GO ?= go
BIN_DIR ?= $(HOME)/.local/bin
INSTALL ?= install

build: build-hnet build-hnetd

build-hnet:
	$(GO) build -o ./hnet ./cmd/hnet

build-hnetd:
	$(GO) build -o ./hnetd ./cmd/hnetd

install: build
	mkdir -p "$(BIN_DIR)"
	$(INSTALL) -m 0755 ./hnet "$(BIN_DIR)/hnet"
	$(INSTALL) -m 0755 ./hnetd "$(BIN_DIR)/hnetd"

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
	rm -f ./hnet ./hnetd
