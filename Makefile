.PHONY: build test web-build web-test go-build go-test

build: web-build go-build

web-build:
	cd web && npm ci && npm run build

go-build:
	go build ./...

test: web-test go-test

web-test:
	cd web && npm test

go-test:
	go test ./...
