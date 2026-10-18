.PHONY: build generate test test-unit test-integration test-e2e

LDFLAGS ?= -s -w
BUILD_SEQUENCE ?=

build:
	@mkdir -p bin
	@VERSION_VALUE=$$(python3 scripts/package-version.py $(if $(BUILD_SEQUENCE),--sequence $(BUILD_SEQUENCE)) --output bin/build-info.json) && \
	COMMIT_VALUE=$$(python3 -c 'import json; print(json.load(open("bin/build-info.json"))["source_sha"])') && \
	DATE_VALUE=$$(python3 -c 'import json; print(json.load(open("bin/build-info.json"))["built_at"])') && \
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS) -X main.Version=$$VERSION_VALUE -X main.Commit=$$COMMIT_VALUE -X main.Date=$$DATE_VALUE" -trimpath -o bin/server ./cmd/server

generate:
	go generate ./ent
	go generate ./cmd/server

test:
	go test ./...
	golangci-lint run ./...

test-unit:
	go test -tags=unit ./...

test-integration:
	go test -tags=integration ./...

test-e2e:
	./scripts/e2e-test.sh

test-e2e-local:
	go test -tags=e2e -v -timeout=300s ./internal/integration/...
