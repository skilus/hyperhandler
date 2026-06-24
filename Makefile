# hyperhandler — Go port (SPEC-007)

GO        ?= go
BINARY    ?= hyperhandler
CMD       := ./cmd/hyperhandler
VERSION   ?= 0.4.0-dev
LDFLAGS   := -X main.version=$(VERSION)

# Python venv used only to regenerate golden vectors (oracle = official HL SDK).
PYTHON    ?= .venv/bin/python

# Release matrix. The SQLite driver is modernc.org/sqlite (pure Go), so every
# target builds fully static with CGO_ENABLED=0.
DIST      ?= dist
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build test cover lint tidy golden clean fmt vet release $(PLATFORMS)

all: build

build: ## Build the static binary (no cgo)
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

test: ## Run all Go tests
	$(GO) test ./... -count=1

cover: ## Run tests with per-package coverage
	$(GO) test ./... -cover

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

release: $(PLATFORMS) ## Cross-compile static binaries for all platforms into $(DIST)/

$(PLATFORMS):
	@os=$(word 1,$(subst /, ,$@)); arch=$(word 2,$(subst /, ,$@)); \
	ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	out=$(DIST)/$(BINARY)-$(VERSION)-$$os-$$arch$$ext; \
	echo "building $$out"; \
	CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -ldflags "$(LDFLAGS)" -o $$out $(CMD)

clean:
	rm -rf bin $(DIST)
