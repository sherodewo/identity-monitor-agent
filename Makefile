GO ?= go
SOURCE ?= generic_web_search
SUBJECT ?= subject-001
DATE ?=
.PHONY: setup setup-indobert run demo test test-race lint build migrate browser-bootstrap run-once report
setup:
	$(GO) mod download
setup-indobert:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/setup-indobert.ps1
run:
	$(GO) run ./cmd/collector serve
demo:
	$(GO) run ./cmd/collector demo
test:
	$(GO) test ./...
test-race:
	$(GO) test -race ./...
lint:
	golangci-lint run
build:
	$(GO) build -ldflags '-linkmode=internal' -o bin/identity-monitor-agent ./cmd/collector
migrate:
	$(GO) run ./cmd/collector migrate
browser-bootstrap:
	$(GO) run ./cmd/collector browser bootstrap --source=$(SOURCE)
run-once:
	$(GO) run ./cmd/collector run-once --subject=$(SUBJECT) --source=$(SOURCE)
report:
	$(GO) run ./cmd/collector report --date=$(DATE)
