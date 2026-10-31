# Implementation handoff

Use this file to implement one bounded task at a time. The full design is in [backend.md](backend.md); the assignment requirements are in [the PDF](Computer_Network_Term_Project___Socket_Programming.pdf). PostgreSQL is required. Complete the backend before the frontend. Implement R1-R11 plus the requested account/multiple-device support; defer special features.

## Working rules for each task

1. Inspect the current branch, existing files, `git status`, and applicable `AGENTS.md`. Preserve unrelated work; do not assume a previous attempt left files behind.
2. Read this file and only the relevant sections of `backend.md`. Implement only the selected task and its necessary tests.
3. Reuse the contracts below. Do not introduce another database, authentication scheme, transport, framework, or naming convention.
4. Keep business rules in services and SQL in `internal/store`. HTTP and WebSocket handlers call services; they do not duplicate authorization rules.
5. Run the task's checks. Do not mark it complete if its required checks are skipped or blocked.
6. Update the status table with changed files, checks actually run, and any blocker. End with a short handoff for the next task. Do not automatically start the next task.

Suggested prompt for a smaller model:

> Read plan.md. Implement task T[number] only, following its dependencies and acceptance checks. Inspect existing work first. Update the status table and report the checks actually run. Stop at that task boundary.

## Current status

| Task | Status | Evidence / handoff |
| --- | --- | --- |
| T1 schema | Complete | Added `apps/api/internal/store/migrations/001_initial.sql`, `tests/schema.sql`, and store `README.md`; refined `backend.md` data model. Acceptance SQL passed on PostgreSQL 18.4 in isolated `t1_schema_acceptance` schema, including required constraints and message/counter rollback; verified the test schema was removed by rollback. No blocker. |
| T2 database setup and migrations | Complete | Added `apps/api/internal/config`, `internal/store/database.go`, `migrations.go`, integration tests, database-aware `internal/httpapi/handler.go`, startup wiring, `compose.yaml`, PostgreSQL init script, environment examples, Go dependency files, and setup docs (`README.md`, store README, `backend.md`). PostgreSQL 18.4: fresh/repeated/concurrent migrations, failed-migration rollback, pool bound, config validation, outage/startup failure, and reopen persistence passed via `go test -count=1 ./...` with dedicated `TEST_DATABASE_URL`. Live `/health` returned 200, then 503 while Compose PostgreSQL stopped, then 200 after restart; migration record and test marker persisted. `go vet ./...` and `go build -buildvcs=false ./...` passed; test schemas/marker removed. Optional `go test -race` could not build because host MinGW `cc1.exe` lacks 64-bit support; this is not a T2 acceptance blocker. |
| T3 backend_authorization | Complete | Added `apps/api/internal/auth` (Argon2id password policy/hash verification, session tokens and trusted identity, rate/hash workload limits, synchronous revocation notifier), `internal/store/users.go` (account/session SQL and atomic signup/switching), `internal/httpapi/auth_handlers.go` (signup/login/me/logout, cookies, JSON limits, exact Origin/CORS), config/startup wiring, tests, `COOKIE_SECURE` sample, and setup docs. On PostgreSQL 18.4, `go test -count=1 ./...` with dedicated `TEST_DATABASE_URL` passed signup/login success and generic failure, duplicate-name race, hash verification, cookie attributes, expiry/revocation, both signup and login account switching, other-session survival, notifier ordering, Origin/CORS and body rejection, token/hash secrecy, rate limits, bounded hash queue, and account/session rollback. `go vet ./...`, `go build -buildvcs=false ./...`, and `git diff --check` passed; no `t3_` test schemas remain. No blocker. |
| T4 backend_rooms | Complete | Added `apps/api/internal/chat/{types,service,service_test}.go`, `internal/store/{rooms,messages}.go`, and updated `README.md`. Services accept trusted T3 identity, create canonical direct rooms with exactly two memberships, create creator-only groups, allow self-join, expose metadata-only discovery and own-room lists, authorize sends/history, deduplicate retries, and page history with join boundaries and a fixed high-water mark. PostgreSQL 18.4 acceptance passed with `TEST_DATABASE_URL`: concurrent A-B/B-A opens, C isolation, group membership/discovery, repeat join, concurrent join/send ordering, concurrent sends, cross-room retry conflicts, pagination, and message/counter rollback. `go test -count=1 ./...`, five repeat T4 concurrency runs, final targeted rollback/Unicode validation tests, `go vet ./...`, plain `go build ./...`, and Git whitespace checks passed. No `t4_` test schemas remain; new API files and directory are Admin-owned. T5 should call these services with validated session identity, broadcast only when `SendResult.Created`, and enforce its protocol response-size cap. No blocker. |
| T5 backend_websocket | Complete | Added `apps/api/internal/realtime` with authenticated exact-Origin `GET /ws`, post-upgrade session recheck, per-session revocation, expiry and command-time validation, single-owner connection indexes, bounded queues, heartbeat, rate/connection limits, ordered command dispatch, authorized event delivery, capped history responses, and explicit shutdown. Wired the HTTP router/API entry point, expanded `backend.md` protocol payloads and root `README.md`. Real WebSocket acceptance tests against isolated PostgreSQL 18.4 schemas passed for handshakes, expired/revoked sessions, A on multiple devices plus B/C, presence, private/group isolation, logout with another session surviving, reconnect history, invalid frames and close codes, queue overflow, and shutdown. `go test -count=1 ./...`, `go vet ./...`, `go test -race -count=1 ./...` (64-bit UCRT GCC), `go build ./...`, and Git whitespace checks passed. No `t5_` test schemas remain. T5 files and nested Git metadata are Admin-owned; T6 remains untouched. No blocker. |
| T6 frontend | Not started | Depends on the verified HTTP/WebSocket contracts in T5. |
| T7 integration and assignment demonstration | Not started | Depends on T6. |

A previous schema attempt reached a PostgreSQL 18 test but expected `23503` for an explicit `ON DELETE RESTRICT` violation; that server returned `23001`. Automatic approval then failed because of a usage limit. That attempt is not evidence that the current checkout has a completed schema. A disposable Docker container named `network-chat-schema-a7f31c9b` may remain from it: inspect its status before using or cleaning it up, and do not remove unrelated containers or volumes. No successful cleanup has been confirmed.

## Fixed architecture and behavior

- Backend: Go, `net/http`, `pgx/v5/pgxpool`, PostgreSQL 18, `coder/websocket`, and Argon2id from `golang.org/x/crypto/argon2`. Pin compatible versions during T2/T3; update the Go minimum based on their actual requirements.
- Frontend: the existing Next.js App Router and TypeScript application. Use native browser WebSocket.
- Database location: `apps/api/internal/store/migrations`. A database is infrastructure owned by the backend, so it does not need an `apps/db` application.
- HTTP handles authentication and health. WebSocket handles presence, room/group commands, message sending, and message history.
- User identity is an account. A session is a browser login. A connection is one tab/device socket. Membership is attached to users, not connections.
- A private room has exactly two participants. A group starts with only its creator. Users join groups themselves; no endpoint accepts another user's ID to add them.
- All authenticated users may see group metadata and member lists. Only room members may access message content. A group member's history begins after their stored join sequence.
- Messages go to every connected device of authorized participants, including the sender's devices. Online users are deduplicated by account and include the requesting user.
- Store messages before acknowledging them. Retry identifiers prevent duplicate inserts. Acknowledgments confirm storage, not delivery or reading.
- A single realtime coordinator owns connection maps and orders chat operations. Database constraints remain authoritative. Multi-server coordination is deferred.

## T1: schema

Read `backend.md` sections 4-5 and 8. Create:

```text
apps/api/internal/store/
  migrations/001_initial.sql
  tests/schema.sql
  README.md
```

Use PostgreSQL SQL, UUID IDs, `timestamptz` timestamps, `bigint` sequences, and `bytea` token digests. IDs are application supplied. Define named constraints so services can map database errors reliably. The migration has no internal `BEGIN`/`COMMIT`; its caller owns the transaction. Do not use `IF NOT EXISTS` to hide conflicting schema definitions.

| Table | Required fields and constraints |
| --- | --- |
| `users` | `id`, unique canonical `username`, `password_hash`, `created_at`. Username must match lowercase ASCII `[a-z0-9_]{3,24}`. |
| `sessions` | `id`, `user_id` FK, unique 32-byte `token_hash`, `created_at`, `expires_at`, nullable `revoked_at`. Expiry is later than creation. Index user and expiry. |
| `rooms` | `id`, `kind`, nullable group `name`, `created_by` FK, `direct_user_low`/`direct_user_high` user FKs, generated nullable `direct_key`, `last_seq`, `created_at`. |
| `room_members` | Primary key `(room_id, user_id)`, both FKs, `joined_at`, `joined_seq`. Index `(user_id, room_id)`. |
| `messages` | `id`, `room_id`, `sender_id`, UUID `client_message_id`, `seq`, `text`, `created_at`. Unique `(room_id, seq)` and `(sender_id, client_message_id)`. Composite FK `(room_id, sender_id)` references membership. |

Schema refinement to incorporate into `backend.md`: direct rooms store two typed user references in sorted UUID order. Generate `direct_key` from their canonical UUID strings joined with `:` and make it unique. Both references must be present and distinct, and the creator must be one of them. Groups have a nonblank 1-64-code-point name and null direct-user fields/key. Direct rooms have a null name. This prevents duplicates and invalid pair references without parsing arbitrary text keys.

Bound `last_seq` and `joined_seq` to 0..9007199254740991, and message `seq` to 1..9007199254740991. Text length is 1-2,000 code points. Reject ordinary blank-space content at the database boundary; the service will perform full Unicode whitespace validation. Do not duplicate indexes already created by unique constraints. Prefer restricted deletion because account/group deletion is out of scope.

Document service-owned invariants: atomic insertion of initial memberships; exactly the designated two members in a private room; self-join authorization; room sequence allocation; join/history boundary; password hashing; and identity derived from sessions. SQL table checks alone do not enforce these cross-row/access rules.

Acceptance: apply to an isolated PostgreSQL 18 schema and test valid records, username/token uniqueness, invalid room shapes and pairs, duplicate memberships, nonmember message inserts, retry uniqueness across rooms, text/sequence limits, and transactional rollback of both a message and its room counter. SQL tests roll back their fixtures and test schema. Record the actual database version and results. Add an ER diagram and execution instructions to the store README. Do not implement login or sockets in this task.

## T2: database setup and migrations

Read `backend.md` sections 3, 5, 12-13. Add config parsing, a bounded pgx pool, embedded migration loading/version tracking, and database-aware `/health`. Add PostgreSQL Compose setup and native-install instructions. Use a dedicated application role/database and a persistent named volume; bind the database's published port to loopback. Keep secrets in ignored environment files.

Required config: `DATABASE_URL`, `DATABASE_MAX_CONNS` (default 10), `HOST`, `PORT`, `ALLOWED_ORIGINS`. Never log credentials or full URLs. Ping on startup and fail clearly when PostgreSQL is unreachable. Serialize migration startup with a transaction-scoped advisory lock. Apply the migration and its version record atomically.

Store methods use transactions consistently and parameterized `$1` SQL. Use isolated schemas in a dedicated database specified by `TEST_DATABASE_URL`; never fall back to the application's database for destructive test setup.

Acceptance: fresh/repeated/concurrent migration runs, restart persistence, failed migration rollback, database outage, invalid config, and bounded pool acquisition. Database unavailability returns readiness failure. Update setup docs and Go dependency/version files.

## T3: backend_authorization

Read `backend.md` section 6. This task includes both authentication (who the user is) and reusable authorization inputs (the trusted user/session identity).

Implement `internal/auth`, account/session store methods, and HTTP handlers:

| Route | Contract |
| --- | --- |
| `POST /auth/signup` | Validate and normalize username; hash password; create account/session atomically; return `201` and public user; set cookie. |
| `POST /auth/login` | Verify credentials; return `200` and public user; issue a fresh session cookie. Invalid credentials use a generic `401`. |
| `GET /auth/me` | Return `{user, expiresAt}` or `401`. |
| `POST /auth/logout` | Revoke current session; clear cookie; idempotent `204`. |

Passwords: 15-128 code points, at most 512 UTF-8 bytes, no silent whitespace trimming. Use salted Argon2id per `backend.md`. Session tokens are 32 random bytes; store only their SHA-256 digest. Use host-only HttpOnly cookies, SameSite=Lax, seven-day fixed expiry, and Secure in HTTPS mode. Cookie tokens are not JSON response fields or local-storage values.

Add exact Origin validation for browser mutation routes, credentialed CORS, body limits, generic errors, and bounded login/signup/hash workloads. Use the same hostname for frontend HTTP and backend HTTP/WS URLs. Normal tabs share their browser session; distinct devices typically have different sessions.

Expose reusable session validation and a session-revocation notification interface for T5. Account switching revokes the old browser session. When T5 is added, logout must wait for removal of that session's sockets; do not claim active-socket revocation is complete before then.

Acceptance: signup/login success and failure, duplicate-name races, hash verification, cookie attributes, session expiry/revocation, account switching, CORS/Origin rejection, and no token/hash leakage. Authorization always uses server-derived identity, never a supplied `senderId`.

## T4: backend_rooms

Read `backend.md` sections 5 and 8-9. Implement `internal/chat` services and room/membership/message store methods independently of WebSocket types.

- Direct open: validate the other account, reject self-chat, canonicalize pair, find/create one room with exactly two memberships atomically. Existing conversations remain usable when a participant is offline.
- Group create: accept only a name, create the room and creator membership atomically. Reject attempts to add other accounts.
- Group join: accept only room ID, join as the trusted caller, and preserve the original join boundary on repeated requests.
- Discovery: online users will come from T5; group catalogue/member metadata may be viewed by any authenticated user. Own-room lists contain only memberships of the caller.
- Message send: verify membership; lock the room row; deduplicate by `(sender_id, client_message_id)`; allocate `last_seq + 1`; insert and commit. Return data for T5 delivery only after commit.
- A reused retry ID with different normalized content or room returns a conflict. A duplicate-key race must roll back the attempted counter increment before reading the original message.
- History: require membership and apply `seq > joined_seq` for groups. Provide bounded pages ordered by sequence, exclusive before/after cursors, and a high-water mark for recovery. Expose `historyStartSeq` so inaccessible earlier messages are not mistaken for gaps.

Join and message insertion must lock the same room row to establish their ordering. Use a non-sequence room counter, so rolled-back sends do not leave committed sequence gaps. Keep SQL and authorization errors separate from HTTP/WebSocket response formatting.

Acceptance: concurrent A-B/B-A opens, isolation from C, creator-only groups, self-join and repeat-join behavior, unauthorized send/history, concurrent join/send boundary, retries/conflicts, pagination, and rollback. Group discovery must never return chat text or private-room metadata.

## T5: backend_websocket

Read `backend.md` sections 7 and 9-11. Implement `internal/realtime`; wire authenticated `GET /ws` into the HTTP router.

Before upgrading, validate the session and an exact allowed Origin. Bind the accepted socket to trusted user/session IDs. Recheck session validity at registration to cover logout races. Close sockets on session expiry and remove every connection of a revoked session, while leaving that account's other sessions intact.

Registry indexes: `connections[connectionID]`, `byUser[userID][connectionID]`, and `bySession[sessionID][connectionID]`. One coordinator owns these maps. Each client has read/write goroutines, cancellation, heartbeat, and a bounded outbound queue. Slow clients are disconnected without blocking healthy clients. Explicitly close upgraded connections during server shutdown.

Freeze envelopes as `{v: 1, type, requestId?, payload}`. Command replies are `request.ok` or `request.error` carrying the original request ID. Unsolicited events omit it. Use the exact payload definitions in `backend.md`; update that document and tests together if a necessary correction is found.

Commands: `users.list`, `rooms.list`, `groups.list`, `groups.members`, `direct.open`, `group.create`, `group.join`, `message.send`, `room.history`.

Events: `session.ready`, `presence.changed`, `group.created`, `group.member_joined`, `room.upserted`, `message.created`.

Presence changes only on the first connection or last disconnection of an account. Private room events go to its participants; message events go to eligible members' devices. Public group events contain metadata only. The requesting device receives the storage acknowledgment; all authorized devices, including the sender's, receive the message event.

Acceptance: real WebSocket handshakes with cookies and Origin; unauthenticated/expired sessions; A on two devices plus B and C; presence transitions; private/group confidentiality; logout across shared-session tabs; other-session survival; reconnect history; invalid frames; queue overflow; and shutdown. Complete protocol examples and run backend tests, vet, race checks, and build before frontend work starts.

## T6: frontend

Read the verified HTTP/WebSocket contracts from T5. Implement the existing Next.js app using these contracts, without adding a second backend.

1. Add sign-up/sign-in pages, session lookup, credentialed HTTP calls, logout, and auth navigation. A page redirect is not backend authorization.
2. Add a reusable WebSocket connection layer for the active login: status, bounded reconnect backoff, cleanup, request correlation, and handling expiry/revocation. No token in the WebSocket URL.
3. Display each online account once, including yourself; selecting another account opens its direct room.
4. Display all groups and their members; create a group and join as yourself. Never offer an add-other-members control.
5. Give every room a separate message list and composer. Load authorized history and merge live messages by ID/sequence. Do not mix messages from different rooms.
6. Generate a UUID per outgoing message and preserve it across retries. Reconcile acknowledgment/live events without duplicate bubbles. Retry group creation only with an explicit user action after checking the catalogue.
7. Refresh presence/catalogue/rooms on reconnect. Recover missing committed messages via history. Handle empty, loading, validation-error, disconnected, and expired-session states.

Use `NEXT_PUBLIC_API_URL` and `NEXT_PUBLIC_WS_URL` with matching hostnames. Keep secrets out of public variables. Render message text as text, never injected HTML.

Acceptance: production build, lint/type checks, signup/signin/logout, separate rooms, creator-only groups/self-join, keyboard-usable forms, private/group privacy from the UI, duplicate-free message reconciliation, reload/reconnect, and two devices for one account.

## T7: integration and assignment demonstration

This gate is needed in addition to unit tests and frontend builds.

- Run real PostgreSQL, Go, and Next.js together; verify R1-R11 against the PDF.
- Use A and B on two different physical computers. Use C to prove a nonparticipant cannot receive private/group content.
- Verify an additional A device receives A's conversations, presence remains correct, and session-specific logout works.
- Restart Go and PostgreSQL while retaining database storage; verify durable accounts/rooms/messages and reconnect behavior.
- Exercise inaccessible room IDs, expired sessions, message retries, and a connection lost around acknowledgment.
- Record commands and observed results; do not claim a two-computer test from two tabs on one computer.
- Prepare the required Implementation Explanation slide and demonstration checklist, including architecture, socket upgrade, TCP/IP endpoints, room routing, and authorization.

Special features remain deferred. Backend completion does not by itself complete the frontend chat-window requirements or physical-computer demonstration.
