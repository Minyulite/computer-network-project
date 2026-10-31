# Computer Network chat project

This repository has a Go HTTP API in `apps/api` and a Next.js frontend in `apps/web`. The backend uses PostgreSQL 18. The current implementation is at T3 in [plan.md](plan.md): database setup and HTTP account sessions are implemented; room and chat features are later tasks.

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

The API exposes `POST /auth/signup`, `POST /auth/login`, `GET /auth/me`, and `POST /auth/logout`. Send JSON credentials to signup/login and an exact `Origin` from `ALLOWED_ORIGINS` on browser mutation requests. Auth responses set a host-only HttpOnly `chat_session` cookie; browser fetches must use `credentials: "include"`. Use the same hostname for the frontend and API URLs, even if their ports differ. Session tokens are never returned in JSON. Logout revokes the current browser session; real-time socket removal is added in T5.

Run the database integration tests only with `TEST_DATABASE_URL` pointing to the separate `chat_test` database:

```sh
cd apps/api
go test ./...
```

The tests create random schemas inside `chat_test` and remove them after use. They never fall back to `DATABASE_URL`. See [the store README](apps/api/internal/store/README.md) for the standalone T1 SQL schema check.

## Start the frontend

```sh
cd apps/web
npm install
npm run dev
```

Open `http://localhost:3000`. The current frontend is a starter until T6. Keep the frontend's hostname consistent with the backend URL when browser authentication is added. The Go module path uses `example.com` as a placeholder until the repository has a public module path.
