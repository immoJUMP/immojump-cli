VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X github.com/immoJUMP/immojump-cli/internal/cli.Version=$(VERSION)

.PHONY: build test lint docs docs-check ci install clean release-build

build:
	go build -ldflags "$(LDFLAGS)" -o bin/immojump ./cmd/immojump

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt nötig:" && gofmt -l . && exit 1)
	go vet ./...

docs: build
	./bin/immojump docs > REFERENCE.md

# REFERENCE.md ist eingecheckt und wird generiert — beides gleichzeitig geht
# nur, wenn Drift auffällt statt sich anzusammeln.
docs-check: docs
	@git diff --exit-code REFERENCE.md \
		|| (echo "REFERENCE.md ist veraltet — 'make docs' laufen lassen und einchecken" && exit 1)

# Das komplette Gate: identisch lokal und in der CI.
ci: lint test docs-check

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/immojump

# Cross-Builds für Agent-Runtimes (Linux-Container) und macOS.
release-build:
	GOOS=linux  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/immojump-linux-amd64 ./cmd/immojump
	GOOS=linux  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/immojump-linux-arm64 ./cmd/immojump
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/immojump-darwin-arm64 ./cmd/immojump
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/immojump-darwin-amd64 ./cmd/immojump

clean:
	rm -rf bin dist coverage.out
