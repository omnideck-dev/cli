BINARY     := omnideck
MODULE     := github.com/omnideck-dev/cli
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GOVULNCHECK_VERSION := v1.6.0
STATICCHECK_VERSION  := v0.7.0
ACTIONLINT_VERSION   := v1.7.12

LDFLAGS := -ldflags "\
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.date=$(DATE) \
"

.PHONY: build test race vet fmt-check tidy-check staticcheck actionlint lint vulnerability verify clean release hardware-test

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "These Go files need gofmt:"; \
		echo "$$files"; \
		exit 1; \
	fi

tidy-check:
	go mod tidy -diff

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) -checks=all,-ST1005 ./...

actionlint:
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

lint: fmt-check tidy-check vet staticcheck actionlint

vulnerability:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

verify: lint test vulnerability

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

hardware-test:
	./tests/hardware/run.sh
