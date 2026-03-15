.PHONY: build build-hnet build-hnetd test vet fmt tidy verify clean

GO ?= go

build: build-hnet build-hnetd

build-hnet:
	$(GO) build -o ./hnet ./cmd/hnet

build-hnetd:
	$(GO) build -o ./hnetd ./cmd/hnetd

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
