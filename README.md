# Computer Network chat project

This repository has a Go API in `apps/api` and a Next.js frontend in `apps/web`, backed by PostgreSQL 18. Commonroom supports sign-up/sign-in, private conversations, self-joined groups, online presence, multiple devices and reconnect recovery. See [plan.md](plan.md) for acceptance status and [the demonstration guide](docs/demonstration.md) for the two-computer setup and required-feature checklist.

## Requirements

- Go 1.26 or newer
- Node.js 20.9 or newer and npm
- PostgreSQL 18, either through Docker Compose or a native installation

## PostgreSQL with Docker Compose

Copy `.env.example` to `.env` at the repository root and set separate strong values for `POSTGRES_ADMIN_PASSWORD` and `CHAT_APP_PASSWORD`. `.env` is ignored by Git. Then run:

```sh
docker compose up -d postgres
docker compose ps
```

The first startup creates a `chat_app` login role, an application database `chat`, and a separate integration-test database `chat_test`. The application role owns both databases and is not a PostgreSQL superuser. PostgreSQL data lives in the `chat_postgres_data` named volume, mounted at `/var/lib/postgresql` for the PostgreSQL 18 image. The database port is published only on `127.0.0.1:5432`. Changing the bootstrap passwords in `.env` after first initialization does not change existing database credentials; update the role in PostgreSQL if needed. `docker compose down` keeps the volume and its data. Do not remove the volume unless you intend to delete the development database.

## Native PostgreSQL 18 setup

As a PostgreSQL administrator, create a non-superuser application role and two databases:

```sql
CREATE ROLE chat_app LOGIN PASSWORD 'replace_with_app_password';
CREATE DATABASE chat OWNER chat_app;
CREATE DATABASE chat_test OWNER chat_app;
```

Run those statements individually, outside a transaction, because `CREATE DATABASE` cannot run inside one. Configure PostgreSQL to accept the application's local connection, then set `DATABASE_URL` and `TEST_DATABASE_URL` to the matching databases. Keep PostgreSQL on loopback for local development. For a remote database, configure TLS certificate validation instead of `sslmode=disable`.

## Start the API

Set the variables from `apps/api/.env.example` in your shell, replacing the password. The Go program reads process environment variables and does not load an `.env` file automatically. `DATABASE_URL` and `ALLOWED_ORIGINS` are required; `DATABASE_MAX_CONNS` defaults to 10, `HOST` to `127.0.0.1`, and `PORT` to 8080. `COOKIE_SECURE` defaults to false for local HTTP and must be true when TLS terminates before the API. The database URL must point to `chat`, not `chat_test`.

If the application password contains URL-reserved characters, percent-encode it in `DATABASE_URL` and `TEST_DATABASE_URL`. In a database client's separate password field, enter the original password without URL encoding. Changing `.env` after PostgreSQL has initialized does not change the role passwords stored in the named volume; update the roles with SQL if you rotate them.

```sh
cd apps/api
go run ./cmd/api
```

Startup pings PostgreSQL and applies embedded migrations under a transaction-scoped advisory lock. It exits with an error if PostgreSQL is unavailable or migration fails. `GET http://127.0.0.1:8080/health` returns `{"status":"ok"}` while the database is reachable and HTTP 503 with `{"status":"unavailable"}` when it is not.

SQL files use LF line endings via `.gitattributes` so embedded migration checksums remain identical across Windows and Unix checkouts. Do not edit an already-applied migration or replace its checksum to silence an error; add a new migration for schema changes.

The API exposes `POST /auth/signup`, `POST /auth/login`, `GET /auth/me`, and `POST /auth/logout`. Send JSON credentials to signup/login and an exact `Origin` from `ALLOWED_ORIGINS` on browser mutation requests. Auth responses set a host-only HttpOnly `chat_session` cookie; browser fetches must use `credentials: "include"`. Use the same hostname for the frontend and API URLs, even if their ports differ. Session tokens are never returned in JSON. Logout revokes the current browser session and closes its WebSocket connections; another session for the same account stays connected.

Authenticated clients connect to `GET /ws` using the session cookie and an exact allowed `Origin`. Commands and events use JSON text frames. The contract, payloads, pagination, recovery, and limits are in [backend.md](backend.md#7-websocket-protocol). All chat content and history travel through WebSocket.

Run the database integration tests only with `TEST_DATABASE_URL` pointing to the separate `chat_test` database:

```sh
cd apps/api
go test ./...
go vet ./...
go build ./...
go test -race ./...
```

The tests create random schemas inside `chat_test` and remove them after use. They never fall back to `DATABASE_URL`. On Windows, the race check needs a 64-bit C compiler; this machine has `C:\msys64\ucrt64\bin\gcc.exe`, which can be selected with `CC` and added to `PATH`. See [the store README](apps/api/internal/store/README.md) for the standalone T1 SQL schema check.

## Start the frontend

```sh
cd apps/web
npm ci
# Copy .env.example to .env.local and set the two public URLs if needed.
npm run dev
```

Open `http://localhost:3000` and create an account. `NEXT_PUBLIC_API_URL` defaults to `http://localhost:8080` and `NEXT_PUBLIC_WS_URL` to `ws://localhost:8080/ws`. Use the same hostname for the website, HTTP API and WebSocket. Never put database credentials or session tokens in public variables. Restart development or rebuild production after changing these variables. The Go module path uses `example.com` as a placeholder until the repository has a public module path.

Choose an online person to open a private room. Select a group to inspect its members, then join it yourself. Existing private rooms remain in the sidebar when their other participant is offline. Group history starts after you joined. A second device signed into the same account receives the same authorized conversations; signing out revokes only the current browser session.

Drafts and unconfirmed sends stay in memory while reconnecting. Retry uses the original message ID so a lost acknowledgment cannot create a second stored message. Refreshing or closing a tab discards unsent drafts; committed messages are recovered from PostgreSQL. If a group creation is not confirmed, refresh and check the catalogue before explicitly creating another group.

## Frontend and integration validation

```sh
cd apps/web
npm run lint
npm run typecheck
npm run build
npx playwright install chromium
# Set TEST_DATABASE_URL to the separate chat_test database first.
npm run test:e2e
```

The E2E runner starts production Next.js and Go against an isolated schema, tests separate browser sessions, privacy, retry/reconnect behavior, session expiry and Go restart, then cleans up. Set `E2E_RESTART_POSTGRES=1` to also restart this project's Compose PostgreSQL service during the test. Reports and screenshots go to ignored `playwright-report` and `test-results` folders. After the suite, rebuild with normal frontend URLs before starting the app normally.

For the physical-computer requirement, follow [docs/demonstration.md](docs/demonstration.md). The [Implementation Explanation slide](docs/implementation-explanation.html) can be opened directly and printed to PDF. Automated browser contexts on one machine do not replace the physical demonstration.
