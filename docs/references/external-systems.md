# External Systems Reference

## Purpose

- Record durable notes about external systems, APIs, providers, or design sources that recur across features
- Keep feature-specific reference details in feature docs as canonical front matter references

## Current State

- PostgreSQL is the production-shaped ledger backend. Local integration targets use the `postgres` Compose profile and `postgres:16-alpine`.
- SQLite is the default local ledger backend and does not require an external service.
- S3-compatible storage is the production-shaped raw-envelope archive backend. Local integration targets use the `localstack` Compose profile and `localstack/localstack:3`.
- Filesystem storage is the default local raw-envelope archive backend and does not require an external service.
- The default standalone Compose file is intentionally usable without any sibling repository or external Compose stack.

## Credentials And Safety

- Do not commit real database credentials, AWS credentials, bearer tokens, or `.env` files.
- Example configs may contain environment-variable placeholders only.
- LocalStack test credentials must stay dummy values such as `test`.
- Missing configured secret environment variables must fail config loading instead of producing empty tokens.
