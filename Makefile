# hyperhandler — Go port (SPEC-007)

GO        ?= go
BINARY    ?= hyperhandler
CMD       := ./cmd/hyperhandler
VERSION   ?= 0.4.0-dev
LDFLAGS   := -X main.version=$(VERSION)

# Python venv used only to regenerate golden vectors (oracle = official HL SDK).
PYTHON    ?= .venv/bin/python

.PHONY: all build test lint tidy golden clean fmt vet

all: build

build: ## Build the static binary (no cgo)
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

test: ## Run all Go tests
	$(GO) test ./... -count=1

vet: ## go vet
	$(GO) vet ./...

fmt: ## gofmt check
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.venv/*'))" || \
		(echo "gofmt needed:"; gofmt -l $$(find . -name '*.go' -not -path './.venv/*'); exit 1)

lint: ## golangci-lint (must be installed)
	golangci-lint run ./...

tidy: ## go mod tidy
	$(GO) mod tidy

golden: ## Regenerate testdata/golden/ from the official HL SDK (D5 oracle)
	PYTHONPATH=src $(PYTHON) tools/goldengen/generate.py

clean:
	rm -rf bin
