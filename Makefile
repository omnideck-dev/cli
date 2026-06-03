BINARY     := omnideck
MODULE     := github.com/omnideck-dev/cli
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "\
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.date=$(DATE) \
"

.PHONY: build test vet lint clean release

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

lint:
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

clean:
	rm -f $(BINARY) dist/*

release:
	mkdir -p dist
	GOOS=linux  GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY) . && tar -czf dist/$(BINARY)-linux-amd64.tar.gz  -C dist $(BINARY)
	GOOS=linux  GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY) . && tar -czf dist/$(BINARY)-linux-arm64.tar.gz  -C dist $(BINARY)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY) . && tar -czf dist/$(BINARY)-darwin-amd64.tar.gz -C dist $(BINARY)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY) . && tar -czf dist/$(BINARY)-darwin-arm64.tar.gz -C dist $(BINARY)
	rm -f dist/$(BINARY)
	@echo "Archives written to dist/"
