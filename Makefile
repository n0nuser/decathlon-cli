.PHONY: build test vet lint fmt check install docs docs-check

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/decathlon ./cmd/decathlon

test:
	go test ./... -count=1

vet:
	go vet ./...

lint:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

fmt:
	gofmt -w .

check: vet lint test
	test -z "$$(gofmt -l .)"

install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/decathlon

# The command reference on the docs site is generated from the cobra tree.
# docs-check (run in CI) fails when the committed pages lag the binary.
docs:
	go run ./cmd/docsgen

docs-check: docs
	git diff --exit-code -- docs-site/content/docs/reference
	test -z "$$(git ls-files --others --exclude-standard -- docs-site/content/docs/reference)"
