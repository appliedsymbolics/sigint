# Testing Reference

## Purpose

- Record durable repo-wide testing guidance that is broader than one feature
- Keep feature-specific testing details in the current feature's `SPEC.md` Validation Map and Evidence sections; legacy staged flows may still use `PLAN.md` or `TASKS.md`

## Current State

- Use `make check` as the default pre-delivery validation path. It regenerates OpenAPI docs, formats Go code, tidies modules, runs `go vet ./...`, and runs `go test ./...`.
- Use `make smoke` to prove the local SQLite/filesystem runtime path, server startup, readiness, docs routes, event ingest, event lookup, and shutdown.
- Use `docker compose config --services` to validate the standalone Compose file without requiring the Docker daemon to be healthy.
- Use `make test-postgres` when PostgreSQL-backed ledger behavior changes.
- Use `make test-s3-localstack` when S3 storage behavior changes.
- Use `make smoke-production-profile` for end-to-end PostgreSQL plus LocalStack validation when Docker is available.

## Required Evidence

Feature specs should record the exact command and result for every relevant validation command. Do not claim Docker-backed checks passed unless Docker was available and the command ran.

## Focused Packages

- API routes and middleware: `go test ./internal/api`
- Config loading and validation: `go test ./internal/config`
- Event contract and hashing: `go test ./internal/events`
- Ingest behavior and retention: `go test ./internal/ingest`
- Ledger behavior: `go test ./internal/ledger`
- Storage adapters: `go test ./internal/storage`
