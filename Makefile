GO ?= $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
GOMODCACHE ?= $(CURDIR)/.cache/go-mod
GOCACHE ?= $(CURDIR)/.cache/go-build
SOURCE ?= generic_web_search
SUBJECT ?= subject-001
DATE ?=
.PHONY: setup setup-indobert setup-indobert-macos run demo test test-race lint build migrate browser-bootstrap run-once report
setup:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) mod download
setup-indobert:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/setup-indobert.ps1
setup-indobert-macos:
	sh scripts/setup-indobert.sh
run:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) run ./cmd/collector serve
demo:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) run ./cmd/collector demo
test:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) test ./...
test-race:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) test -race ./...
lint:
	golangci-lint run
build:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) build -ldflags '-linkmode=internal' -o bin/identity-monitor-agent ./cmd/collector
migrate:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) run ./cmd/collector migrate
browser-bootstrap:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) run ./cmd/collector browser bootstrap --source=$(SOURCE)
run-once:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) run ./cmd/collector run-once --subject=$(SUBJECT) --source=$(SOURCE)
report:
	GOMODCACHE=$(GOMODCACHE) GOCACHE=$(GOCACHE) $(GO) run ./cmd/collector report --date=$(DATE)
