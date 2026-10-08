# Support Ticket API — Go + PostgreSQL

Production-oriented REST API with:

- RBAC (`salesperson`, `support`)
- JWT authentication
- WebSocket push without polling
- multipart evidence uploads (images, audio, video, PDF)
- local storage for development and S3-compatible storage for persistent production files
- PostgreSQL indexes and an event/audit table
- KPI endpoint
- Support-only user registration
- automatic SQL migration on startup

## Run locally

1. Start PostgreSQL from the repository root:

```bash
docker compose up -d postgres
```

2. Copy `.env.example` to `.env` and set a real `JWT_SECRET`.

3. Install dependencies and start:

```bash
go mod tidy
go run ./cmd/server
```

The first start creates the support admin configured by `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` when that email does not already exist.

## API

- `POST /api/auth/login`
- `GET /api/auth/me`
- `GET /api/tickets`
- `POST /api/tickets` (multipart field `evidence`, repeated for multiple files)
- `GET /api/tickets/:id`
- `PATCH /api/tickets/:id/status` (support only)
- `GET /api/files/:id`
- `GET /api/kpi` (support only)
- `POST /api/users` (support only)
- `GET /api/users` (support only)
- `GET /ws`
- `GET /healthz`

## WebSocket authentication

The browser sends the JWT in `Sec-WebSocket-Protocol` as `ticket-auth.<JWT>`, avoiding a token in the query string. Events are JSON objects with `type`, `entity`, `id` and `data`.

## File storage

Local storage is convenient for development. In production, use `STORAGE_DRIVER=s3` with an S3-compatible bucket so files survive container replacement and horizontal scaling.
