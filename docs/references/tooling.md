# Tooling Reference

## Purpose

- Record durable repo-wide tooling notes, command references, and local development expectations
- Keep short-lived implementation notes in feature docs instead of here

## Current State

- `make help` lists operator and developer entrypoints.
- `make docs` regenerates Swaggo OpenAPI output under `docs/`.
- `make build` builds `bin/sigint` from `cmd/sigint`.
- `make run` starts the foreground server using `examples/config.local.yaml`.
- `make run-bg`, `make status`, and `make stop` exercise background server lifecycle behavior.
- `make db-init` initializes the configured ledger with GORM AutoMigrate.
- `make post-event` posts `examples/sample-event.json` to the running API.
- `make smoke` runs the default local SQLite/filesystem smoke flow.
- `docker compose up -d` starts the standalone local app service.
- `docker compose --profile postgres up -d postgres` starts the optional PostgreSQL service.
- `docker compose --profile localstack up -d localstack` starts the optional S3-compatible LocalStack service.

## Environment

- Use `SIGINT_CONFIG` when loading config from environment.
- Use `SIGINT_ENV=debug` only for local debug routes and debug stream inspection.
- Use `SIGINT_PRODUCER_TOKEN` and `SIGINT_INTERNAL_TOKEN` for bearer auth when configured by YAML.
- Use `SIGINT_POSTGRES_DSN`, `SIGINT_S3_BUCKET`, `SIGINT_AWS_REGION`, `SIGINT_S3_ENDPOINT`, `SIGINT_S3_SERVER_SIDE_ENCRYPTION`, and `SIGINT_S3_KMS_KEY_ID` for production-shaped config expansion.
