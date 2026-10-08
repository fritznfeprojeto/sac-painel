# Architecture and contracts

## Request flow

```text
Browser SPA
  │
  ├── REST /api/* ──────────────> Go HTTP router
  │                               ├── JWT auth middleware
  │                               ├── RBAC middleware
  │                               ├── PostgreSQL transaction
  │                               └── Storage adapter
  │
  └── WebSocket /ws ───────────> Realtime Hub
                                  └── broadcasts ticket.created / ticket.updated
```

## RBAC matrix

| Capability | Salesperson | Support |
|---|---:|---:|
| Login | Yes | Yes |
| Create ticket | Yes | Yes |
| View own tickets | Yes | Yes |
| View all tickets | No | Yes |
| View evidence | Own only | All |
| Change ticket status | No | Yes |
| Resolve/reject with message | No | Yes |
| KPI dashboard | No | Yes |
| Register users | No | Yes |

## Routes

### Auth
- `POST /api/auth/login` — verifies bcrypt password and returns JWT.
- `GET /api/auth/me` — validates the active user.

### Tickets
- `GET /api/tickets` — Support sees all; Salesperson is automatically constrained to own tickets.
- `POST /api/tickets` — multipart; repeat `evidence` for multiple files.
- `GET /api/tickets/:id` — authorization checks the ticket creator for Salesperson.
- `PATCH /api/tickets/:id/status` — Support only; `resolved` and `rejected` require a non-empty resolution message.

### Evidence
- `GET /api/files/:id` — ownership check for Salesperson, then streams from local storage or S3-compatible storage.

### Support administration
- `GET /api/kpi` — aggregate open/in-progress/resolved/rejected and average resolution time.
- `POST /api/users` — register Salesperson or Support.
- `GET /api/users` — list registered users.

### Realtime
- `GET /ws` — JWT is carried in the WebSocket subprotocol as `ticket-auth.<JWT>`.
- Event types:
  - `ticket.created`
  - `ticket.updated`

Event shape:

```json
{
  "type": "ticket.updated",
  "entity": "ticket",
  "id": "uuid",
  "data": { "ticketCode": "TCK-202610-000001" }
}
```

## File storage model

Each ticket gets its own logical folder:

```text
tickets/<ticket-id>/<random-id>-<safe-original-name>
```

The database stores the immutable relationship in `ticket_files`. The application does not expose storage keys to the browser.

### Development
`STORAGE_DRIVER=local` writes under `LOCAL_STORAGE_PATH`.

### Production
`STORAGE_DRIVER=s3` writes to an S3-compatible bucket using the `S3_*` variables. This is preferred for Railway/container deployments because object storage is independent of an individual application instance.

## PostgreSQL performance choices

- `idx_tickets_status_updated`: supports queue/status ordering.
- `idx_tickets_creator_created`: supports salesperson history.
- `idx_tickets_open_priority`: partial index for the active support queue.
- `idx_ticket_files_ticket`: fast folder/evidence lookup by ticket.
- `idx_ticket_events_*`: supports audit/event retrieval.
- Ticket list aggregates evidence count in one SQL statement instead of N+1 queries.

## Transaction boundary for ticket creation

1. Begin transaction.
2. Insert ticket and obtain sequence number.
3. Build human ticket code.
4. Upload each evidence object.
5. Insert each evidence metadata row.
6. Insert `ticket.created` audit event.
7. Commit PostgreSQL transaction.
8. Broadcast `ticket.created` over WebSocket only after commit.

If object storage or metadata persistence fails, already-uploaded objects are removed on a best-effort basis and the transaction is rolled back.

## Frontend real-time strategy

The SPA makes normal REST calls for durable state and uses the WebSocket only for event delivery. A received ticket event updates local state and triggers the active view to refresh from the API when required. There is no periodic HTTP polling loop.
