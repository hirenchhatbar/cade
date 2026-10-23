APP   := cade
PART  ?= patch
UID   := $(shell id -u)
GID   := $(shell id -g)
GO    := HOST_UID=$(UID) HOST_GID=$(GID) docker compose run --rm go

.DEFAULT_GOAL := help
.PHONY: help setup build run test fmt vet tidy shell image clean bump

help: ## List targets
	@grep -E '^[a-z]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'

setup: ## Build the dev image
	docker compose build go

build: ## Compile binary to bin/
	$(GO) env CGO_ENABLED=0 go build -o bin/$(APP) ./cmd/$(APP)

run: ## Run the app
	$(GO) go run ./cmd/$(APP)

test: ## Run tests
	$(GO) go test ./...

fmt: ## Format code
	$(GO) go fmt ./...

vet: ## Static checks
	$(GO) go vet ./...

tidy: ## Tidy go.mod
	$(GO) go mod tidy

shell: ## Shell in the dev container
	$(GO) bash

image: ## Build production image
	docker build -t $(APP):latest .

clean: ## Remove build output and cache volume
	rm -rf bin
	docker compose down -v --rmi local

bump: ## Bump version (PART=patch|minor|major, or V=x.y.z)
	@f=cmd/$(APP)/main.go; \
	cur=$$(sed -n 's/^\tversion *= *"\([^"]*\)"$$/\1/p' $$f); \
	[ -n "$$cur" ] || { echo "bump: version const not found in $$f" >&2; exit 1; }; \
	new="$(V)"; \
	if [ -z "$$new" ]; then \
		new=$$(echo "$$cur" | awk -F. -v p="$(PART)" '\
			p=="major" { print $$1+1 ".0.0"; next } \
			p=="minor" { print $$1 "." $$2+1 ".0"; next } \
			p=="patch" { print $$1 "." $$2 "." $$3+1; next } \
			{ exit 1 }') || { echo "bump: PART must be patch, minor or major" >&2; exit 1; }; \
	fi; \
	echo "$$new" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "bump: bad version '$$new'" >&2; exit 1; }; \
	sed -i "s/^\(\tversion *= *\)\"$$cur\"$$/\1\"$$new\"/" $$f; \
	echo "$$cur -> $$new"
