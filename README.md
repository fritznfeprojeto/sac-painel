# Support Ticket Monitoring System

Complete starter/production-oriented implementation using:

- Backend: Go 1.23 + REST API + WebSocket hub
- Frontend: Vanilla JavaScript + HTML + CSS
- Database: PostgreSQL 17 schema with indexes
- Auth: JWT + RBAC (`salesperson`, `support`)
- Evidence: image/audio/video/PDF uploads
- Real-time: native WebSocket push; no 1-second polling
- Deployment target: Railway-friendly Dockerfiles

## Repository layout

- `backend/` — Go API, auth, RBAC, WebSocket hub, SQL migrations and storage adapters
- `frontend/` — static SPA UI
- `postgres/` — standalone PostgreSQL schema package
- `docker-compose.yml` — local PostgreSQL

## Local startup

### 1. Database

```bash
docker compose up -d postgres
```

### 2. Backend

```bash
cd backend
cp .env.example .env
# Change JWT_SECRET and, before first start, the seed admin credentials.
go mod tidy
go run ./cmd/server
```

### 3. Frontend

Serve `frontend/` with a static HTTP server:

```bash
cd frontend
python -m http.server 5500
```

For a separate local API, add this before the module script in `frontend/index.html`:

```html
<script>window.SUPPORT_API_BASE = 'http://localhost:8080';</script>
```

Open `http://localhost:5500`.

## First login

Use the values from `SEED_ADMIN_EMAIL` and `SEED_ADMIN_PASSWORD` in `backend/.env`. The backend inserts that Support user only when the email does not exist.

## Production storage

Use `STORAGE_DRIVER=s3` with an S3-compatible bucket for persistent evidence. This avoids tying uploaded files to a single application container and works with S3-compatible providers. Local storage is intended for development or a deployment with a persistent mounted volume.

## Railway deployment pattern

Deploy the backend as a Docker service using `backend/Dockerfile`. Provide PostgreSQL through Railway's PostgreSQL service or a compatible hosted Postgres and set `DATABASE_URL`. For evidence, prefer S3-compatible object storage with the `S3_*` variables. Deploy the static frontend with `frontend/Dockerfile` or host its files on a static host, then set the API origin through `window.SUPPORT_API_BASE` when frontend and API are separated.

Use `ALLOWED_ORIGINS` to list the exact frontend origin(s).

See `ARCHITECTURE.md` for the RBAC matrix, route contract, WebSocket event format, file storage flow and PostgreSQL performance design.
