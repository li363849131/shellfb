BINARY=shellfb
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

.PHONY: build test run clean

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/shellfb

test:
	go test ./... -v -race

run: build
	./bin/$(BINARY)

clean:
	rm -rf bin/
