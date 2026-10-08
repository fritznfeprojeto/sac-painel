# PostgreSQL package

The schema is safe to run multiple times and creates the RBAC, tickets, evidence and audit/event tables used by the Go API.

Local development is easiest with the root `docker-compose.yml`.

Default local database values:
- database: `support_tickets`
- user: `support`
- password: `change-me`
- port: `5432`
