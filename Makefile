SHELL := /bin/bash

.DEFAULT_GOAL := help

BIN_DIR ?= bin
BINARY_NAME ?= sigint
SIGINT_BIN ?= $(BIN_DIR)/$(BINARY_NAME)
CONFIG ?= examples/config.local.yaml
EVENT_FILE ?= examples/sample-event.json
HOST ?= 127.0.0.1
PORT ?= 8920
BASE_URL ?= http://$(HOST):$(PORT)
STATUS_URL ?= $(BASE_URL)/readyz
PID_FILE ?= var/sigint.pid
LOG_FILE ?= var/sigint.log
PRODUCTION_SMOKE_PORT ?= 8931
PRODUCTION_SMOKE_BASE_URL ?= http://127.0.0.1:$(PRODUCTION_SMOKE_PORT)
POSTGRES_USER ?= sigint
POSTGRES_PASSWORD ?= sigint
POSTGRES_DB ?= sigint
POSTGRES_PORT ?= 54329
POSTGRES_DSN ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@127.0.0.1:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
LOCALSTACK_PORT ?= 45660
LOCALSTACK_ENDPOINT ?= http://127.0.0.1:$(LOCALSTACK_PORT)
CLEAN_PATHS ?= bin tmp var
GO_FILES := $(shell find cmd internal -type f -name '*.go' | sort)

.PHONY: help docs fmt tidy vet test build run dev run-bg status stop db-init post-event smoke check clean compose-up compose-down compose-config compose-config-production test-postgres test-s3-localstack smoke-production-profile

help: ## Show this help menu.
	@awk 'BEGIN {FS = ":.*##"; printf "sigint make targets:\n\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

docs: ## Generate Swagger/OpenAPI documentation.
	@printf "generating OpenAPI docs\n"
	@GO111MODULE=on go run github.com/swaggo/swag/cmd/swag@v1.16.4 init \
		-g main.go \
		--dir ./cmd/sigint,./internal/api,./internal/events \
		--parseDependency --parseInternal \
		--output ./docs 2>&1 | grep -v 'failed to evaluate const mProfCycleWrap'

fmt: ## Format Go source files.
	gofmt -w $(GO_FILES)

tidy: ## Update Go module files.
	GO111MODULE=on go mod tidy

vet: ## Run Go vet.
	GO111MODULE=on go vet ./...

test: ## Run all Go tests.
	GO111MODULE=on go test ./...

build: ## Build the sigint binary.
	@mkdir -p $(BIN_DIR)
	@printf "building %s\n" "$(SIGINT_BIN)"
	@GO111MODULE=on go build -o $(SIGINT_BIN) ./cmd/sigint

run: build ## Start the API server in the foreground.
	@printf "starting %s on %s:%s (config: %s)\n" "$(BINARY_NAME)" "$(HOST)" "$(PORT)" "$(CONFIG)"
	@"$(SIGINT_BIN)" server start --config $(CONFIG) --host $(HOST) --port $(PORT)

dev: ## Start the API server with Air hot reload.
	air -c .air.toml

run-bg: build ## Start the API server in the background.
	@mkdir -p "$(dir $(PID_FILE))" "$(dir $(LOG_FILE))"
	@"$(SIGINT_BIN)" server start \
		--config $(CONFIG) \
		--host $(HOST) \
		--port $(PORT) \
		--background \
		--pid-file $(PID_FILE) \
		--log-file $(LOG_FILE)

status: build ## Check API readiness.
	@"$(SIGINT_BIN)" server status --url $(STATUS_URL)

stop: build ## Stop the background API server.
	@"$(SIGINT_BIN)" server stop --pid-file $(PID_FILE)

db-init: build ## Initialize the configured ingest ledger.
	@"$(SIGINT_BIN)" db init --config $(CONFIG)

post-event: ## Post the sample event to the running HTTP API.
	@curl -fsS -X POST "$(BASE_URL)/v1/events" \
		-H "Content-Type: application/json" \
		--data @$(EVENT_FILE)

smoke: build ## Run a local HTTP and CLI smoke test with a temporary runtime directory.
	@set -euo pipefail; \
	tmpdir=$$(mktemp -d); \
	cleanup() { \
		if [ -f "$$tmpdir/sigint.pid" ]; then \
			"$(SIGINT_BIN)" server stop --pid-file "$$tmpdir/sigint.pid" >/dev/null 2>&1 || true; \
		fi; \
		rm -rf "$$tmpdir"; \
	}; \
	trap cleanup EXIT; \
	config="$$tmpdir/config.yaml"; \
	printf '%s\n' \
		'server:' \
		'  host: 127.0.0.1' \
		'  port: 8929' \
		'ledger:' \
		'  adapter: sqlite' \
		"  path: $$tmpdir/ingest.sqlite" \
		'storage:' \
		'  adapter: filesystem' \
		"  root: $$tmpdir/event-lake" \
		'ingest:' \
		'  reject_hash_conflicts: true' \
		'  require_payload_hash: true' \
		'  require_event_hash: true' \
		> "$$config"; \
	"$(SIGINT_BIN)" db init --config "$$config" >/dev/null; \
	"$(SIGINT_BIN)" server start --config "$$config" --host 127.0.0.1 --port 8929 --background --pid-file "$$tmpdir/sigint.pid" --log-file "$$tmpdir/sigint.log" >/dev/null; \
	ready=0; \
	for _ in 1 2 3 4 5 6 7 8 9 10; do \
		if "$(SIGINT_BIN)" server status --url "http://127.0.0.1:8929/readyz" >/dev/null 2>&1; then \
			ready=1; \
			break; \
		fi; \
		sleep 0.5; \
	done; \
	if [ "$$ready" != "1" ]; then \
		cat "$$tmpdir/sigint.log" >&2; \
		exit 1; \
	fi; \
	curl -fsS http://127.0.0.1:8929/ >/dev/null; \
	curl -fsS http://127.0.0.1:8929/llms.txt >/dev/null; \
	curl -fsS http://127.0.0.1:8929/v1/docs/swagger/index.html >/dev/null; \
	curl -fsS http://127.0.0.1:8929/openapi.json >/dev/null; \
	curl -fsS -X POST http://127.0.0.1:8929/v1/events -H "Content-Type: application/json" --data @$(EVENT_FILE) >/dev/null; \
	"$(SIGINT_BIN)" events get --config "$$config" --event-id 3ee6c93d-1f50-4e65-a867-f2f998be9ada >/dev/null; \
	"$(SIGINT_BIN)" server stop --pid-file "$$tmpdir/sigint.pid" >/dev/null; \
	trap - EXIT; \
	rm -rf "$$tmpdir"; \
	printf "smoke ok\n"

check: docs fmt tidy vet test ## Generate docs, format, tidy, vet, and test.

clean: ## Remove local runtime artifacts.
	@set -euo pipefail; \
	if [ -f "$(PID_FILE)" ]; then \
		$(MAKE) --no-print-directory stop PID_FILE="$(PID_FILE)" >/dev/null 2>&1 || true; \
	fi; \
	rm -rf $(CLEAN_PATHS); \
	printf "cleaned: %s\n" "$(CLEAN_PATHS)"

compose-up: ## Start the local app container.
	docker compose up -d

compose-down: ## Stop and remove local compose services.
	docker compose down

compose-config: ## Render compose service names.
	docker compose config --services

compose-config-production: ## Render compose services with Postgres and LocalStack profiles.
	docker compose --profile postgres --profile localstack config --services

test-postgres: ## Run Postgres-backed integration tests against the compose Postgres service.
	@set -euo pipefail; \
	if ! docker info >/dev/null 2>&1; then \
		printf "Docker is required for make test-postgres. Start Docker and retry.\n" >&2; \
		exit 1; \
	fi; \
	SIGINT_POSTGRES_DB="$(POSTGRES_DB)" SIGINT_POSTGRES_USER="$(POSTGRES_USER)" SIGINT_POSTGRES_PASSWORD="$(POSTGRES_PASSWORD)" SIGINT_POSTGRES_PORT="$(POSTGRES_PORT)" docker compose --profile postgres up -d postgres; \
	ready=0; \
	for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do \
		if docker compose exec -T postgres pg_isready -U "$(POSTGRES_USER)" -d "$(POSTGRES_DB)" >/dev/null 2>&1; then \
			ready=1; \
			break; \
		fi; \
		sleep 1; \
	done; \
	if [ "$$ready" != "1" ]; then \
		docker compose logs postgres >&2; \
		exit 1; \
	fi; \
	SIGINT_POSTGRES_TEST_DSN="$(POSTGRES_DSN)" GO111MODULE=on go test ./internal/ledger ./internal/ingest

test-s3-localstack: ## Run S3 adapter tests with LocalStack infrastructure available.
	@set -euo pipefail; \
	if ! docker info >/dev/null 2>&1; then \
		printf "Docker is required for make test-s3-localstack. Start Docker and retry.\n" >&2; \
		exit 1; \
	fi; \
	SIGINT_LOCALSTACK_PORT="$(LOCALSTACK_PORT)" docker compose --profile localstack up -d localstack; \
	ready=0; \
	for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do \
		if curl -fsS "$(LOCALSTACK_ENDPOINT)/_localstack/health" >/dev/null 2>&1; then \
			ready=1; \
			break; \
		fi; \
		sleep 1; \
	done; \
	if [ "$$ready" != "1" ]; then \
		docker compose logs localstack >&2; \
		exit 1; \
	fi; \
	AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test SIGINT_AWS_REGION=us-east-1 SIGINT_S3_TEST_ENDPOINT="$(LOCALSTACK_ENDPOINT)" GO111MODULE=on go test ./internal/storage

smoke-production-profile: build ## Run an end-to-end production-profile smoke test against Compose Postgres and LocalStack.
	@set -euo pipefail; \
	if ! docker info >/dev/null 2>&1; then \
		printf "Docker is required for make smoke-production-profile. Start Docker and retry.\n" >&2; \
		exit 1; \
	fi; \
	SIGINT_POSTGRES_DB="$(POSTGRES_DB)" SIGINT_POSTGRES_USER="$(POSTGRES_USER)" SIGINT_POSTGRES_PASSWORD="$(POSTGRES_PASSWORD)" SIGINT_POSTGRES_PORT="$(POSTGRES_PORT)" SIGINT_LOCALSTACK_PORT="$(LOCALSTACK_PORT)" docker compose --profile postgres --profile localstack up -d postgres localstack; \
	postgres_ready=0; \
	for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do \
		if docker compose exec -T postgres pg_isready -U "$(POSTGRES_USER)" -d "$(POSTGRES_DB)" >/dev/null 2>&1; then \
			postgres_ready=1; \
			break; \
		fi; \
		sleep 1; \
	done; \
	if [ "$$postgres_ready" != "1" ]; then \
		docker compose logs postgres >&2; \
		exit 1; \
	fi; \
	localstack_ready=0; \
	for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do \
		if curl -fsS "$(LOCALSTACK_ENDPOINT)/_localstack/health" >/dev/null 2>&1; then \
			localstack_ready=1; \
			break; \
		fi; \
		sleep 1; \
	done; \
	if [ "$$localstack_ready" != "1" ]; then \
		docker compose logs localstack >&2; \
		exit 1; \
	fi; \
	tmpdir=$$(mktemp -d); \
	smoke_id=$$(date +%s)-$$RANDOM; \
	smoke_db="sigint_smoke_$$(printf '%s' "$$smoke_id" | tr -c '[:alnum:]' '_')"; \
	smoke_bucket="sigint-smoke-$$(printf '%s' "$$smoke_id" | tr -c '[:alnum:]' '-' | tr '[:upper:]' '[:lower:]')"; \
	cleanup() { \
		if [ -f "$$tmpdir/sigint.pid" ]; then \
			"$(SIGINT_BIN)" server stop --pid-file "$$tmpdir/sigint.pid" >/dev/null 2>&1 || true; \
		fi; \
		docker compose exec -T postgres dropdb -U "$(POSTGRES_USER)" --if-exists "$$smoke_db" >/dev/null 2>&1 || true; \
		rm -rf "$$tmpdir"; \
	}; \
	trap cleanup EXIT; \
	docker compose exec -T postgres createdb -U "$(POSTGRES_USER)" "$$smoke_db"; \
	docker compose exec -T localstack awslocal s3api create-bucket --bucket "$$smoke_bucket" >/dev/null; \
	smoke_dsn="postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@127.0.0.1:$(POSTGRES_PORT)/$$smoke_db?sslmode=disable"; \
	config="$$tmpdir/production.config.yaml"; \
	printf '%s\n' \
		'server:' \
		'  host: 127.0.0.1' \
		"  port: $(PRODUCTION_SMOKE_PORT)" \
		'ledger:' \
		'  adapter: postgres' \
		"  dsn: $$smoke_dsn" \
		'storage:' \
		'  adapter: s3' \
		"  bucket: $$smoke_bucket" \
		'  prefix: raw' \
		'  region: us-east-1' \
		"  endpoint: $(LOCALSTACK_ENDPOINT)" \
		'  force_path_style: true' \
		'ingest:' \
		'  reject_hash_conflicts: true' \
		'  require_payload_hash: true' \
		'  require_event_hash: true' \
		'replay:' \
		'  default_limit: 100' \
		'  max_limit: 1000' \
		> "$$config"; \
	export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test SIGINT_AWS_REGION=us-east-1; \
	"$(SIGINT_BIN)" db init --config "$$config" >/dev/null; \
	"$(SIGINT_BIN)" diagnostics ready --config "$$config" > "$$tmpdir/diagnostics.json"; \
	grep -q '"status":"ready"' "$$tmpdir/diagnostics.json"; \
	"$(SIGINT_BIN)" server start --config "$$config" --host 127.0.0.1 --port $(PRODUCTION_SMOKE_PORT) --background --pid-file "$$tmpdir/sigint.pid" --log-file "$$tmpdir/sigint.log" >/dev/null; \
	server_ready=0; \
	for _ in 1 2 3 4 5 6 7 8 9 10; do \
		if "$(SIGINT_BIN)" server status --url "$(PRODUCTION_SMOKE_BASE_URL)/readyz" >/dev/null 2>&1; then \
			server_ready=1; \
			break; \
		fi; \
		sleep 0.5; \
	done; \
	if [ "$$server_ready" != "1" ]; then \
		cat "$$tmpdir/sigint.log" >&2; \
		exit 1; \
	fi; \
	curl -fsS -X POST "$(PRODUCTION_SMOKE_BASE_URL)/v1/events" -H "Content-Type: application/json" --data @$(EVENT_FILE) > "$$tmpdir/ingest.json"; \
	grep -q '"status":"stored"' "$$tmpdir/ingest.json"; \
	curl -fsS -X POST "$(PRODUCTION_SMOKE_BASE_URL)/v1/events" -H "Content-Type: application/json" --data @$(EVENT_FILE) > "$$tmpdir/duplicate.json"; \
	grep -q '"status":"duplicate"' "$$tmpdir/duplicate.json"; \
	curl -fsS "$(PRODUCTION_SMOKE_BASE_URL)/v1/events/3ee6c93d-1f50-4e65-a867-f2f998be9ada" > "$$tmpdir/lookup.json"; \
	grep -q '"storage_uri":"s3://' "$$tmpdir/lookup.json"; \
	curl -fsS "$(PRODUCTION_SMOKE_BASE_URL)/internal/v1/events/replay?limit=1" > "$$tmpdir/http-replay.json"; \
	grep -q '"event_id":"3ee6c93d-1f50-4e65-a867-f2f998be9ada"' "$$tmpdir/http-replay.json"; \
	grep -q '"cursor":1' "$$tmpdir/http-replay.json"; \
	"$(SIGINT_BIN)" events replay --config "$$config" --limit 1 > "$$tmpdir/cli-replay.json"; \
	grep -q '"event_id":"3ee6c93d-1f50-4e65-a867-f2f998be9ada"' "$$tmpdir/cli-replay.json"; \
	"$(SIGINT_BIN)" events get --config "$$config" --event-id 3ee6c93d-1f50-4e65-a867-f2f998be9ada > "$$tmpdir/cli-get.json"; \
	grep -q '"status":"stored"' "$$tmpdir/cli-get.json"; \
	"$(SIGINT_BIN)" retention run --config "$$config" --through-cursor 1 --limit 10 > "$$tmpdir/retention.json"; \
	grep -q '"confirmed":1' "$$tmpdir/retention.json"; \
	grep -q '"deleted":1' "$$tmpdir/retention.json"; \
	"$(SIGINT_BIN)" server status --url "$(PRODUCTION_SMOKE_BASE_URL)/readyz" >/dev/null; \
	"$(SIGINT_BIN)" server stop --pid-file "$$tmpdir/sigint.pid" >/dev/null; \
	trap - EXIT; \
	cleanup; \
	printf "production profile smoke ok\n"
