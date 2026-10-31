# Next.js + Go starter

This repository contains two independent applications:

```text
apps/
  web/  Next.js frontend (TypeScript, App Router)
  api/  Go HTTP API
```

## Requirements

- Node.js 20.9 or newer and npm
- Go 1.22 or newer

## Run locally

Open two terminals at the repository root.

**Terminal 1 — API**

```powershell
cd apps/api
go run ./cmd/api
```

The API listens on `http://localhost:8080` by default. Check `http://localhost:8080/health` for `{"status":"ok"}`. Set `PORT` in the shell to use another port. `apps/api/.env.example` documents the setting; Go does not load `.env` files automatically.

**Terminal 2 — frontend**

```powershell
cd apps/web
npm.cmd install
npm.cmd run dev
```

Open `http://localhost:3000`. The frontend reads `API_BASE_URL` on the server; it defaults to `http://localhost:8080`. Copy `apps/web/.env.example` to `apps/web/.env.local` if you need a different API address. On a shell that runs npm directly, use `npm` instead of `npm.cmd`.

## Project layout

- `apps/web/src/app`: Next.js routes, layouts, and page styles.
- `apps/web/src/lib`: frontend helpers, including calls to the Go API.
- `apps/api/cmd/api`: entry point for the API binary.
- `apps/api/internal/httpapi`: HTTP routes and handlers.

The frontend calls the Go API from the Next.js server, so the browser does not need a separate CORS configuration for this starter. Add new directories when there is code to put in them.

The Go module path in `apps/api/go.mod` uses `example.com` as a placeholder because this repository does not have a remote URL yet. Before publishing the module for other Go projects to download, change it to the real repository path and update Go imports.
