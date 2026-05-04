.PHONY: build test web-build web-test go-build go-test release install

VERSION := $(shell git describe --tags --always 2>/dev/null || echo "0.0.0-dev")
LDFLAGS := -X github.com/bjk95/defrost/internal/cli.DefrostVersion=$(VERSION)

build: web-build go-build

web-build:
	cd web && npm ci && npm run build

go-build:
	go build -o defrost -ldflags "$(LDFLAGS)" ./cmd/defrost
	go build -o defrost-ci -ldflags "$(LDFLAGS)" ./cmd/defrost-ci

test: web-test go-test

web-test:
	cd web && npm test

go-test:
	go test ./...

# release builds both binaries with the same version stamp as a tagged
# build, suitable for distribution.
release: web-build
	go build -o defrost -ldflags "$(LDFLAGS)" ./cmd/defrost
	go build -o defrost-ci -ldflags "$(LDFLAGS)" ./cmd/defrost-ci

# install installs the versioned full binary into $GOPATH/bin (or $GOBIN).
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/defrost
