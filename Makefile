BIN := bin/apm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet lint clean install

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

test:
	go test -count=1 -timeout 120s -race ./...

vet:
	go vet ./...

lint: vet

clean:
	rm -rf bin/

install: build
	cp $(BIN) $(GOPATH)/bin/apm 2>/dev/null || cp $(BIN) $(HOME)/go/bin/apm

all: vet test build
