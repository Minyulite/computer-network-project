# Backend implementation plan

Status: proposed design; backend implementation has not started.

This plan covers the required chat application and the requested account system. PostgreSQL is the selected database, as requested. We will implement and verify the backend first, then build the Next.js frontend against its documented API. Other design choices below are proposals for this project, not extra requirements from the assignment.

## 1. Starting point and scope

The requirements source is [Computer Network Term Project - Socket Programming](Computer_Network_Term_Project___Socket_Programming.pdf), pages 1 and 2.

At the time of this review, the working branch is `main`, although the earlier prototype discussion took place on `test`. The current checkout contains the Next.js starter and a Go `/health` endpoint. It does not contain the prototype chat hub. This document does not switch branches or modify application code.

### Implement now

- Required features R1-R11.
- Registration, login, logout, and authenticated sessions, as requested.
- One account can connect from several devices or tabs.
- Persistent users, sessions, rooms, memberships, and messages to support those account-based conversations and reconnecting.
- Backend tests, protocol documentation, and instructions for a demonstration on two physical computers.

Persistence and session support are implementation choices supporting the requested application. We will not count them as assignment special features.

### Defer

- AI features and cloud deployment.
- Attachments, voice/video calls, reactions, typing indicators, read receipts, and push notifications.
- Email verification, password recovery, social login, and account profile editing.
- Group invitations, adding other users, administrators, removing members, leaving/deleting groups, and message editing/deletion.
- Multiple Go server instances, Redis, and distributed message routing.

The PDF specifies that the later special-feature work must include at least one AI feature. That remains a later phase.

## 2. Requirements and acceptance criteria

| Requirement | Backend responsibility | How we will demonstrate it |
| --- | --- | --- |
| R1: architecture and two physical computers | A Go server reachable over the LAN; explain HTTP, WebSocket, TCP, and IP | Two different accounts connect from two physical computers. The server runs on either computer or a third machine. |
| R2: socket communication for chat | Deliver new messages and room history through WebSocket frames | Inspect WebSocket traffic and verify that chat content is not sent through REST endpoints. |
| R3: unique client names | Enforce a unique account username in the database | Duplicate usernames are rejected, including case variations. |
| R4: list connected clients, including self | Return online accounts once each and send presence updates | Connect A on two devices and B on another; each sees A and B once, including themselves. |
| R5: separate private and group rooms | Stable room IDs and membership rules | A-B and A-C have different rooms; reopening A-B returns the same room. Each group has its own room. |
| R6: message input and display in each room | Room metadata, authorized history, acknowledgments, and live events | Backend contract tests first; the chat box/window requirement is completed during frontend work. |
| R7: private messages | Only the two private-room participants can send, read, and receive messages | A and B receive their conversation; C cannot access it, even with its room ID. |
| R8: create groups with only the creator | Atomically create a room and creator membership | Creating a group inserts exactly one member. |
| R9: all groups and their members are discoverable | Authenticated group catalogue and member-list queries | A can inspect the name and members of a group without joining it. |
| R10: clients join themselves | Join derives the joining user from the session | A can join as A; a request to add B is rejected. |
| R11: group messages only for members | Check membership on sending, history reads, and live delivery | Members receive the message; a nonmember receives no message content. |
| Requested accounts | Signup, signin, logout, session expiry | Passwords are hashed; invalid sessions cannot access chat. |
| Requested multiple devices | Associate connections with account IDs and session IDs | Messages reach all intended users' connected devices; one device disconnecting does not sign out the others. |

We interpret a named client in the assignment as an authenticated user. A device/tab is a connection for that user. The main assignment demonstration will still include two distinct users on two physical computers; the multi-device case is an additional scenario.

## 3. Proposed architecture

```mermaid
flowchart LR
    A[Browser A] -->|HTTP authentication| API[Go HTTP router]
    B[Browser B] -->|HTTP authentication| API
    A <-->|WebSocket chat| WS[Go realtime coordinator]
    B <-->|WebSocket chat| WS
    API --> AUTH[Authentication service]
    API -->|Authenticated upgrade| WS
    WS --> AUTH
    WS --> CHAT[Chat service]
    AUTH --> DB[(PostgreSQL)]
    CHAT --> DB
```

Next.js serves the browser application. The browser talks directly to the Go backend for both authentication and WebSockets. Next.js will not relay each chat message.

We will start with one Go process and a PostgreSQL 18 server. For local development, PostgreSQL can run in Docker Compose or as a native installation on the backend computer. Only the Go backend connects to PostgreSQL; browsers communicate with Go through HTTP and WebSocket. PostgreSQL uses its own persistent storage and is a separate service from the Go process.

### Transport choices

- HTTP routes handle account creation, login, logout, session lookup, and health checks.
- WebSocket handles presence, room/group operations, sending messages, and retrieving history.
- The current architecture uses WebSocket over TCP, with an HTTP upgrade at connection time.
- Go connects to PostgreSQL using the PostgreSQL wire protocol over TCP, normally on port 5432. This database connection is separate from browser chat connections.
- A deployed secure configuration uses HTTPS/WSS. The controlled local/LAN demonstration may use HTTP/WS with test accounts.
- No UDP or WebTransport work is planned.

The browser specifies the destination URL, for example `ws://127.0.0.1:8080/ws`. Its operating system selects the source IP and temporary port. For another computer, use the server's LAN IP rather than `localhost`.

### Dependencies

| Tool/package | Purpose |
| --- | --- |
| Go standard library: `net/http`, `encoding/json`, `context`, `crypto/rand`, `crypto/sha256`, `log/slog` | HTTP, JSON, cancellation, random identifiers, session-token hashing, and logging |
| `github.com/coder/websocket` | WebSocket handshake, frames, close handling, and ping/pong |
| `github.com/jackc/pgx/v5` and its `pgxpool` package | Native PostgreSQL access, transactions, and a bounded connection pool |
| `golang.org/x/crypto/argon2` | Argon2id password hashing |

The installed Go toolchain is 1.26.5. T2 pins `pgx/v5` v5.11.0 and declares Go 1.26.0 in `go.mod`; the Argon2id and WebSocket packages will be added when their code is implemented in T3 and T5. The published packages' minimum Go versions must be checked when those dependencies are added.

The dependency choices are based on the [WebSocket package documentation](https://pkg.go.dev/github.com/coder/websocket), [pgx connection-pool documentation](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), and [Argon2 package documentation](https://pkg.go.dev/golang.org/x/crypto/argon2). We will use the native pgx API throughout the store package rather than mixing it with a second database API.

## 4. User, session, connection, and room identities

These identifiers have different jobs:

| Identity | Lifetime | Example |
| --- | --- | --- |
| User ID | Persistent account | A remains the same user across devices and logins. |
| Session ID | One browser login session | A's laptop and phone normally have separate sessions. |
| Connection ID | One active WebSocket | Two tabs sharing a login cookie still open two connections. |
| Room ID | Persistent conversation | A-B private room or a named group. |
| Message ID | Persistent message | Used to reconcile live events and history. |

Use random UUIDs generated on the server for persistent record IDs and connection IDs, stored as PostgreSQL `uuid` values where applicable and represented as strings in JSON. IDs identify records; knowing an ID never grants access.

The frontend may supply `receiverId` when opening a direct conversation. Once a room exists, messages target `roomId`. The server derives `senderId` from the authenticated connection and derives recipients from stored room membership. It rejects attempts to supply another sender or an arbitrary recipient list.

## 5. Data model

Use explicit PostgreSQL SQL through `pgxpool`, with versioned migration files embedded in the Go binary. A migration version table records which migrations have run. Before accepting connections, acquire a transaction-scoped advisory lock, check the migration table, and apply pending initial-schema migrations and version records in that transaction. Concurrent startup attempts therefore cannot apply the same migration simultaneously. Roll back on failure and do not start serving with a partially migrated schema.

Use `uuid` for IDs and their foreign keys, `timestamptz` for timestamps, `bigint` for room sequences, `bytea` for the session-token digest, and `text` for names, messages, and encoded password hashes. Use `NOT NULL`, foreign keys, unique constraints, and checks for valid room kinds and nonnegative sequences. Preserve nullability for fields such as `revoked_at` and group-only room names. Configure database connections to UTC and encode API timestamps as UTC RFC3339. See PostgreSQL's [data types](https://www.postgresql.org/docs/18/datatype.html) and [timestamp behavior](https://www.postgresql.org/docs/18/datatype-datetime.html).

### `users`

| Column | Purpose |
| --- | --- |
| `id` | Primary key |
| `username` | Canonical, unique username |
| `password_hash` | Encoded Argon2id parameters, salt, and hash |
| `created_at` | Server creation time |

Proposed username rule: trim surrounding whitespace, convert to lowercase, then require 3-24 ASCII letters, digits, or underscores. `Alice` and `alice` therefore refer to the same username. Keep usernames immutable in this phase.

### `sessions`

| Column | Purpose |
| --- | --- |
| `id` | Internal session identifier |
| `user_id` | Foreign key to users |
| `token_hash` | Unique SHA-256 hash of the random cookie token |
| `created_at` | Session creation time |
| `expires_at` | Fixed expiration time |
| `revoked_at` | Null until logout/revocation |

Index `token_hash`, `user_id`, and `expires_at`. Store the token hash rather than the usable cookie token. The fast hash is appropriate for a high-entropy random token; passwords use Argon2id instead.

### `rooms`

| Column | Purpose |
| --- | --- |
| `id` | Primary key |
| `kind` | `direct` or `group` |
| `name` | Group title; null for direct rooms |
| `created_by` | Creator user ID |
| `direct_user_low`, `direct_user_high` | Nullable typed user references; both set in ascending UUID order for direct rooms, both null for groups |
| `direct_key` | Generated unique canonical pair of UUID strings joined with `:` for direct rooms; null for groups |
| `last_seq` | Last committed message sequence number within this room |
| `created_at` | Creation time |

The database requires both direct-user references to exist, differ, and appear in ascending UUID order. The creator must be one of them. It generates `direct_key` from those typed references; its uniqueness constraint prevents A opening A-B while B concurrently creates a second B-A room. Direct rooms have a null name. Groups require a nonblank 1-64-code-point name and null direct-user fields/key. Group names need not be globally unique; group IDs distinguish them.

### `room_members`

| Column | Purpose |
| --- | --- |
| `room_id`, `user_id` | Composite primary key; foreign keys to room and user |
| `joined_at` | Membership creation time |
| `joined_seq` | Room's last sequence at the moment this user joined |

Index by user ID for listing their rooms. Membership belongs to the user and survives logout/disconnection.

Direct-room membership is fixed to the two participants, with `joined_seq = 0`. A group creator starts with `joined_seq = 0`. A later joiner gets the room's current `last_seq`.

Proposed group-history policy: members can read messages sent after they joined, including while they were offline. A new member does not gain access to pre-join messages. This is a deliberate interpretation of R11 and will be documented in the frontend contract.

### `messages`

| Column | Purpose |
| --- | --- |
| `id` | Primary key |
| `room_id` | Foreign key to the conversation |
| `sender_id` | Authenticated sender |
| `client_message_id` | Client-generated retry identifier |
| `seq` | Increasing sequence number within the room |
| `text` | Validated message text |
| `created_at` | Server timestamp |

Add unique constraints on `(room_id, seq)` and `(sender_id, client_message_id)`. The first unique constraint already provides the `(room_id, seq)` index for history pagination and gap recovery. A composite foreign key from `(room_id, sender_id)` to membership enforces sender membership at insertion.

Message sequence allocation and insertion happen in one transaction. Timestamps are for display; `seq` determines message order within a room. JSON IDs are strings, timestamps are UTC RFC3339, and sequences stay within JavaScript's safe integer range.

The schema bounds `last_seq` and `joined_seq` to 0..9007199254740991, and message `seq` to 1..9007199254740991. Message text has 1-2,000 Unicode code points and cannot consist only of ordinary spaces. Services validate full Unicode whitespace. Schema checks do not enforce initial membership counts, self-join authorization, room sequence allocation, join/history boundaries, password hashing, or identity derived from a session; services own those rules.

### Database behavior

- Create one shared `pgxpool.Pool`, initially with a maximum of ten connections. Explicitly ping PostgreSQL at startup; creating a pool alone does not prove the database is reachable. Readiness checks use a bounded database ping.
- Use PostgreSQL's default Read Committed isolation with explicit room-row locks where operations must serialize. Foreign keys are enforced by PostgreSQL without connection-specific activation.
- Both group joining and sending a message lock the relevant room row using `SELECT ... FOR UPDATE` before reading/changing its sequence. This defines the exact join/history boundary even when transactions run concurrently. Keep the lock until commit or rollback.
- Increment `rooms.last_seq` with `UPDATE ... RETURNING last_seq` and insert the message in the same transaction. Rollback restores the room counter. Avoid a standalone PostgreSQL sequence for this counter because recovery expects committed room sequences without allocation gaps.
- For concurrent private-room creation, rely on the unique `direct_key`; use `INSERT ... ON CONFLICT DO NOTHING RETURNING ...`, then a subsequent statement to read the winning room when necessary. Create participant memberships atomically with a new room.
- Enforce message retry uniqueness in PostgreSQL. If a conflicting insert occurs after a pre-check, roll back the whole attempted transaction, including its room counter update, then fetch and compare the committed original message before returning an acknowledgment or conflict.
- Use `$1`, `$2`, and other bound SQL parameters. Complete and close result rows; perform every statement in a transaction through that transaction, not by acquiring another pool connection.
- Bound pool acquisition and database operations with context deadlines. Roll back failures and send a request error. Handle PostgreSQL error codes for uniqueness and referential integrity rather than matching error strings.
- Commit a new message before acknowledging it or sending it to any socket.
- Keep PostgreSQL data in its managed data directory or a Docker named volume. Exclude local credentials, database dumps, local environment files, and caches from Git during implementation.

PostgreSQL's [locking documentation](https://www.postgresql.org/docs/18/explicit-locking.html) describes row locks and transaction-scoped advisory locks. Database constraints and transactions remain authoritative even though the initial realtime coordinator also orders chat commands in memory.

## 6. Authentication and session flow

### Registration and login

Signup accepts `username` and `password`. The proposed password policy is 15-128 Unicode code points, with a 512-byte upper bound. Preserve password whitespace; do not silently trim it. Email is not required.

Hash passwords with Argon2id, a fresh random 16-byte salt, and a 32-byte output. Start with 19 MiB memory, two iterations, and one lane, then benchmark on the demonstration machine. These parameters follow the [OWASP password-storage recommendation](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html). Store parameters alongside the hash, verify in constant time, and bound concurrent hashing work.

Successful signup creates the account and its first session in one transaction, returns the public user, and sets the session cookie. Hash the password before entering that transaction. Login verifies an existing account and issues a fresh session. Incorrect username/password responses use the same generic error. A precomputed dummy hash avoids an obvious cheap lookup path for unknown usernames.

Generate a session token from 32 random bytes. Store its hash and send the original only in an `HttpOnly` cookie. Proposed cookie settings: host-only, `Path=/`, `SameSite=Lax`, and `Secure` in HTTPS mode. Fixed session lifetime: seven days. No JWT or browser local-storage token is needed for this design.

If a browser logs into another account while already authenticated, revoke its old session and close sockets associated with that old session before replacing the cookie. Other devices' sessions remain valid.

### HTTP endpoints

| Method/path | Request | Result |
| --- | --- | --- |
| `GET /health` | None | `200` when the process and database are ready; `503` if not ready |
| `POST /auth/signup` | `{username, password}` | `201`, public user, and session cookie; `409` for duplicate username |
| `POST /auth/login` | `{username, password}` | `200`, public user, and fresh session cookie; `401` for invalid credentials |
| `GET /auth/me` | Session cookie | `200` with `{user, expiresAt}` or `401` |
| `POST /auth/logout` | Session cookie | Idempotent `204`; revoke this session, close its sockets, clear cookie |
| `GET /ws` | Session cookie and allowed Origin | Upgrade authenticated connection or reject before upgrading |

Account mutation routes accept JSON only with a small request-body limit. HTTP errors use `{error: {code, message}}`; malformed input is `400`, unauthenticated access `401`, rejected origin `403`, and rate limiting `429`.

### Browser cookies, origins, and logout

For local development the browser loads Next.js from `http://localhost:3000` and calls Go at `http://localhost:8080`. Fetch requests use `credentials: "include"`. Both URLs use the same hostname, even though their ports differ. Do not mix `localhost` and `127.0.0.1` when relying on the shared host's cookie.

HTTP CORS permits only the exact configured frontend origins, with credentials enabled. Origin checks also apply to signup, login, and logout to prevent cross-site requests. WebSocket upgrades separately validate Origin and session before accepting the connection. Cookie-authenticated browser routes reject a missing or `null` Origin where an Origin is required; test tools send an explicit allowed Origin.

We will explicitly enforce the configured WebSocket origin allowlist. The WebSocket library also permits the request host by default, so library options alone should not define our stricter application policy. Origins identify allowed browser pages; they are not user authentication.

Each socket retains its user ID, session ID, and expiration. Check session validity when registering the socket and processing commands; expire the socket at its session deadline. Logout closes all connections sharing that session and returns only after they have been removed from routing. A separate device's session stays signed in. Normal tabs in the same browser profile usually share one login cookie, so logging out one of those tabs signs out that browser session.

Session expiry and active-socket invalidation are part of authentication, following [OWASP's WebSocket session guidance](https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html).

## 7. WebSocket protocol

Use text frames containing JSON. Set `v: 1` in every envelope. Client commands contain a `type`, a bounded `requestId`, and a `payload`. The request ID correlates a response with an operation on that connection; it is not a durable message identifier.

Example direct-room request:

```json
{
  "v": 1,
  "type": "direct.open",
  "requestId": "req-1",
  "payload": { "receiverId": "user-b-id" }
}
```

The response returns the existing or newly created room. Subsequent messages target that room:

```json
{
  "v": 1,
  "type": "message.send",
  "requestId": "req-2",
  "payload": {
    "roomId": "room-ab-id",
    "clientMessageId": "a-random-uuid-generated-by-the-browser",
    "text": "Hello"
  }
}
```

Successful command response:

```json
{
  "v": 1,
  "type": "request.ok",
  "requestId": "req-2",
  "payload": {
    "messageId": "server-message-id",
    "roomId": "room-ab-id",
    "seq": 12,
    "clientMessageId": "a-random-uuid-generated-by-the-browser"
  }
}
```

Live message event:

```json
{
  "v": 1,
  "type": "message.created",
  "payload": {
    "id": "server-message-id",
    "roomId": "room-ab-id",
    "seq": 12,
    "clientMessageId": "a-random-uuid-generated-by-the-browser",
    "sender": { "id": "user-a-id", "username": "alice" },
    "text": "Hello",
    "sentAt": "2026-10-31T10:00:00Z"
  }
}
```

The event goes to all connected devices of authorized recipients, including the sender. The acknowledgment goes only to the requesting connection. The frontend reconciles the event and acknowledgment by IDs rather than displaying two messages.

### Client commands

| Type | Payload | Behavior |
| --- | --- | --- |
| `users.list` | Pagination options | List distinct online users, including self |
| `rooms.list` | Pagination options | List direct/group rooms the user belongs to, with `latestSeq` and their `historyStartSeq` |
| `groups.list` | Pagination options | List all group summaries; no message contents or previews |
| `groups.members` | `{roomId, ...pagination}` | List members of a group, even if requester has not joined |
| `direct.open` | `{receiverId}` | Find/create the unique room for requester and receiver |
| `group.create` | `{name}` | Create a group with requester as the sole member |
| `group.join` | `{roomId}` | Join as the authenticated requester; repeating it is harmless |
| `message.send` | `{roomId, clientMessageId, text}` | Validate, authorize, persist, acknowledge, and deliver |
| `room.history` | `{roomId, limit, beforeSeq? , afterSeq?, upToSeq?}` | Return only history this user may see |

Paginated catalogue queries use stable ID ordering and opaque cursors. Default page size is 20, maximum 50. Responses include `nextCursor` when another page exists. Individual group member lists are fetched separately so the entire directory need not fit in one frame. The frontend will still make every group's membership viewable as required by R9.

For history, `beforeSeq` and `afterSeq` are exclusive and mutually exclusive. With neither, return the latest page. Return each page in ascending sequence order. Older-history responses provide `nextBeforeSeq`; recovery responses provide `nextAfterSeq`. Return `hasMore` and a fixed `upToSeq` high-water mark for catch-up pagination. Always apply the stored membership boundary, even if the client requests an earlier sequence. `historyStartSeq` is `joined_seq + 1`; the client must not interpret earlier, inaccessible messages as missing deliveries. Also cap serialized response size at 256 KiB, returning fewer records and a continuation cursor as necessary.

### Server events and their audience

| Type | Audience |
| --- | --- |
| `session.ready` | New connection: own user, connection ID, and server instance ID |
| `presence.changed` | All authenticated connections: user ID, username, and online status |
| `group.created` | All authenticated connections: group metadata and initial member |
| `group.member_joined` | All authenticated connections: public group membership update |
| `room.upserted` | Room participants only: metadata for a room added to their own room list |
| `message.created` | Authorized room members only |
| `request.ok` / `request.error` | Requesting connection only |

Group catalogue visibility never grants access to its messages. Global group events contain no message text, excerpts, private room IDs, or private conversation metadata.

Errors include `invalid_payload`, `unknown_type`, `unauthenticated`, `room_unavailable`, `not_a_group`, `rate_limited`, `idempotency_conflict`, and `internal_error`. For history and sending, use the same `room_unavailable` result for a missing room and an inaccessible room to avoid exposing private room existence.

### Verified T5 payloads

Every command has `v: 1`, a `type`, a `requestId` of 1-64 ASCII letters, digits, `_`, or `-`, and a JSON object `payload`. The server replies with the same ID. `request.error` has payload `{ "code": "room_unavailable", "message": "Room unavailable" }` (with a code and safe message appropriate to the failure). Events omit `requestId`.

| Command | Request payload | `request.ok` payload |
| --- | --- | --- |
| `users.list` | `{ "limit"?: 20, "cursor"?: "user-uuid" }` | `{ "items": [{ "id", "username" }], "nextCursor"?: "user-uuid" }` |
| `rooms.list` | Same pagination fields | `{ "items": [room], "nextCursor"?: "room-uuid" }` |
| `groups.list` | Same pagination fields | `{ "items": [group], "nextCursor"?: "group-uuid" }` |
| `groups.members` | `{ "roomId", "limit"?: 20, "cursor"?: "user-uuid" }` | `{ "items": [{ "user": { "id", "username" }, "joinedAt" }], "nextCursor"?: "user-uuid" }` |
| `direct.open` | `{ "receiverId" }` | `{ "room": room, "created": boolean }` |
| `group.create` | `{ "name" }` | `{ "room": room }` |
| `group.join` | `{ "roomId" }` | `{ "room": room, "joined": boolean }` |
| `message.send` | `{ "roomId", "clientMessageId", "text" }` | `{ "messageId", "roomId", "seq", "clientMessageId" }` |
| `room.history` | `{ "roomId", "limit"?: 50, "beforeSeq"?, "afterSeq"?, "upToSeq"? }` | `{ "roomId", "messages": [message], "historyStartSeq", "upToSeq", "hasMore", "nextBeforeSeq"?, "nextAfterSeq"? }` |

`room` has `id`, `kind`, `createdBy`, `latestSeq`, `historyStartSeq`, and `createdAt`; groups also have `name`, while direct rooms have `directUserLow` and `directUserHigh`. `group` has `id`, `name`, `createdBy`, and `createdAt`. A message has the fields shown in the `message.created` example above. The default catalogue page size is 20 (maximum 50); default history size is 50 (maximum 100). An omitted numeric limit or `0` selects the default.

| Event | Payload |
| --- | --- |
| `session.ready` | `{ "user": { "id", "username", "createdAt" }, "connectionId", "serverInstanceId" }` |
| `presence.changed` | `{ "userId", "username", "online": boolean }` |
| `group.created` | `{ "group": group, "initialMember": { "id", "username" } }` |
| `group.member_joined` | `{ "roomId", "member": { "id", "username" } }` |
| `room.upserted` | `{ "room": room }` |
| `message.created` | Message object shown above |

For example, after a new connection, the server sends `{"v":1,"type":"session.ready","payload":{"user":{"id":"user-uuid","username":"alice","createdAt":"2026-10-31T10:00:00Z"},"connectionId":"connection-uuid","serverInstanceId":"server-uuid"}}`. A new group is announced as `{"v":1,"type":"group.created","payload":{"group":{"id":"room-uuid","name":"study","createdBy":"user-uuid","createdAt":"2026-10-31T10:00:00Z"},"initialMember":{"id":"user-uuid","username":"alice"}}}`. These are illustrative IDs; actual IDs are UUIDs.

Unknown commands and invalid payloads receive an error without crashing the connection. Invalid authentication closes it. Reject binary frames and oversized messages with appropriate WebSocket close codes. Do not automatically retry non-idempotent group creation after a lost response; refresh the catalogue and let the user decide whether to create again.

## 8. Room authorization and delivery

### Private conversations

1. A sends `direct.open` with B's account ID.
2. Verify B exists and is not A. Self-conversations are outside this phase.
3. Find/create the canonical direct room and its two memberships in one transaction.
4. Inform A's and B's connected devices of their room.
5. On every send/history request, check membership using the authenticated user ID.
6. Deliver accepted messages only to A's and B's active connections.

The online-user list provides recipients for the initial UI. Existing direct rooms remain available after a participant goes offline. Messages can be stored for later retrieval without adding push notifications.

### Group conversations

1. `group.create` takes only a group name: trim it and require 1-64 Unicode code points.
2. Create room and creator membership atomically. Reject payloads attempting to supply other members.
3. Publish the group's metadata in the global catalogue.
4. `group.join` accepts only a room ID and inserts the caller's membership. Capture the room's current sequence in the same transaction.
5. Repeating a join keeps the original `joined_seq`; it must not reset history visibility.
6. For sending/history, require membership. A user who only views the catalogue is not a member.
7. Deliver a message to all currently connected devices of the members eligible to see that sequence.

Messages before joining remain inaccessible. Membership survives disconnects and server restarts. There is no group owner capability to add another user in this phase.

### A complete send operation

1. Validate session, envelope, rate limit, room ID, text, and client message ID.
2. Verify that the sender belongs to the room.
3. Check `(sender_id, client_message_id)` for an earlier successful send.
4. If it already exists with the same room and normalized text, return its original acknowledgment without inserting or broadcasting again. If its content differs, return `idempotency_conflict`.
5. Otherwise, allocate the next room sequence and insert the message in a transaction.
6. Commit, enqueue the acknowledgment, and enqueue the message event to authorized connections.
7. A slow recipient does not roll back the saved message; it can recover through history.

An acknowledgment means the server saved the message. It is not a delivery/read receipt. A server crash after commit but before event delivery is recovered using room history. The protocol does not claim exactly-once network delivery.

## 9. Multiple devices, presence, and reconnection

The in-memory registry keeps these indexes:

```text
connections[connectionID] -> client
byUser[userID][connectionID] -> client
bySession[sessionID][connectionID] -> client
```

- A's laptop and phone share a user ID but have different sessions and sockets.
- Two ordinary tabs may share a session but still have different sockets.
- Online means at least one authenticated socket is registered.
- Publish online only on the transition from zero connections to one.
- Publish offline only on the transition from one connection to zero.
- Count each account once in `users.list`; do not expose device/session internals to other users.
- Disconnecting a socket does not revoke the login session or remove room membership.
- Logging out revokes the current session and removes all sockets for that session. Other sessions remain online.

On reconnect, the browser authenticates again with its cookie, receives `session.ready`, and refreshes online users, group metadata, and its rooms. Start handling live events before fetching those lists. The coordinator enqueues query responses and state events in their processing order on each connection. Apply them in receive order to maps keyed by stable user/room IDs; do not replay an older event over a newer query response. Refetch a list if changes during pagination prevent a complete view. These lists are current-state views rather than a durable event log.

For messages, retain the last contiguous room sequence locally. Request `room.history` after that sequence and page to the returned high-water mark while buffering/merging live events. Deduplicate by message ID and order by room sequence. Do not skip a missing sequence just because a later event arrived. A new device can load the latest authorized history instead of replaying every message.

Unacknowledged sends may be retried with the same client message ID. Reconnecting does not retry `group.create` automatically. After a Go server restart, connections/presence start empty, but accounts, valid sessions, memberships, rooms, and messages remain in PostgreSQL. Restarting the database service also preserves these records when its persistent storage is retained.

## 10. Concurrency and connection lifecycle

Use one realtime coordinator to own connection indexes and order chat commands for this single-server application. Each connection has a reader goroutine and a writer goroutine. Database work is bounded; password hashing stays outside the coordinator.

```text
socket reader -> decode command -> coordinator -> auth/chat service -> database
                                             -> recipient queues -> socket writers
```

The coordinator serializes chat mutations and their emitted events. This makes concurrent joins, duplicate private-room opens, and per-room message ordering easier to reason about. It is an intentional tradeoff for a small application: a bounded database operation may briefly delay other commands. We can partition work later if measurements justify it.

Readers submit validated envelopes to a bounded command queue. Registration, disconnection, logout, and shutdown use a separate control path so cleanup cannot be trapped behind a queue of user messages. Commands never hold a registry mutex while performing network writes. Only the coordinator mutates its maps.

Each client has a buffered outbound queue of 64 frames. Enqueue without blocking; disconnect a client whose queue fills. A writer applies a five-second timeout to each network write. Cancel a client's context to stop its reader/writer and remove it exactly once. Prefer cancellation without closing outbound channels, avoiding concurrent send/close races.

Use WebSocket ping/pong for dead-connection detection: proposed heartbeat every 30 seconds with a ten-second response timeout. Continue reading while waiting for pong. This bounds how long an abruptly disconnected device remains online; a clean browser close is detected sooner.

For session expiry, use the recorded deadline and command-time validation. Recheck session validity when registering after the handshake to cover logout racing with connection establishment. Live session revocation removes its connections from routing.

On shutdown: stop accepting new requests/upgrades, stop accepting new chat commands, finish or fail pending bounded operations, explicitly close all WebSockets, wait for connection goroutines, then close the database. HTTP shutdown alone does not manage upgraded WebSocket connections.

## 11. Validation and operational limits

These are the initial enforced values, to be exercised in tests rather than treated as performance claims:

| Limit | Proposed initial value |
| --- | --- |
| HTTP auth body | 8 KiB |
| Inbound WebSocket message | 16 KiB |
| Chat text | 1-2,000 Unicode code points after trimming outer whitespace |
| Outbound queue | 64 frames per connection |
| Network write timeout | 5 seconds |
| Database operation deadline | 3 seconds |
| PostgreSQL pool | Maximum 10 connections per Go process |
| Session lifetime | 7 days, fixed |
| Message send rate | 5 per second per user, burst 10, shared across devices |
| Total WebSocket commands | 20 per second per user, burst 40 |
| Login attempts | Bounded by both source IP and canonical username; initially 10 per minute per bucket |
| Signup attempts | 5 per minute per source IP |
| Connections | 10 per user and 200 per server |
| Concurrent password-hash operations | 4 |

Apply reasonable expiry/eviction to rate-limit buckets so arbitrary usernames or disconnected users do not cause unbounded memory growth. Use socket remote addresses for IP limits; do not trust forwarded headers unless a trusted proxy is configured later.

Validate incoming UTF-8, reject unknown command fields where appropriate, and never trust client-supplied identity, membership, timestamps, or sequence numbers. Do not log passwords, cookie tokens, or message bodies. Log connection lifecycle, request type/ID, error code, and server errors without returning internal SQL details to users.

## 12. Proposed package layout

One Go module remains under `apps/api`. These are packages and files inside it, not separate Go modules. Create files as each phase needs them.

```text
apps/api/
  cmd/api/main.go                  # Startup, dependency wiring, shutdown
  internal/
    config/config.go              # Environment parsing and validation
    store/
      database.go                 # PostgreSQL pool, ping, and close
      migrations.go              # Embedded migration runner
      migrations/001_initial.sql
      users.go                    # Account and session SQL records/queries
      rooms.go                    # Rooms and memberships
      messages.go                 # Transactions, deduplication, history
    auth/
      service.go                  # Signup/login/logout/session behavior
      password.go                 # Argon2id encoding and verification
      session.go                  # Token generation and session lookup
    chat/
      service.go                  # Direct/group rules and authorization
      types.go                    # Public chat values and domain errors
    realtime/
      protocol.go                 # JSON envelopes and command payloads
      handler.go                  # Authenticated WebSocket upgrade
      hub.go                      # Connection registry and coordinator
      client.go                   # Socket read/write loops and lifecycle
      dispatch.go                 # Commands -> services -> recipient events
    httpapi/
      router.go                   # HTTP route registration
      auth_handlers.go            # HTTP auth contracts and cookies
      middleware.go               # Origin/CORS, limits, logging
  go.mod
  go.sum
  .env.example
```

At the repository root, add a `compose.yaml` and a root `.env.example` during the foundation phase for the local PostgreSQL service and its development credentials. Application code continues to live under `apps/api` and `apps/web`.

Dependency direction: `httpapi` uses `auth` and `realtime`; `realtime` uses `auth` and `chat`; `auth` and `chat` use `store`. `store` owns database records and does not import these higher-level packages. This prevents import cycles. Business services do not depend on WebSocket connection types.

The earlier prototype put transport and room broadcasting in one `chat.Hub`. The real application separates room rules from socket routing so a global broadcast cannot accidentally deliver private messages. Use clearly named methods for public presence/catalogue events and authorized room delivery.

## 13. Configuration and local network setup

Planned backend settings:

```dotenv
HOST=127.0.0.1
PORT=8080
DATABASE_URL=postgres://chat_app:replace_with_local_password@127.0.0.1:5432/chat?sslmode=disable
DATABASE_MAX_CONNS=10
ALLOWED_ORIGINS=http://localhost:3000
COOKIE_SECURE=false
SESSION_TTL=168h
LOG_LEVEL=info
```

Validate settings at startup. Log the API listen address and a sanitized database host/port/database name; never log the full connection URL or its password. `DATABASE_URL` is required, with no built-in database password. Go environment samples document settings; the application will not silently load `.env` files unless we explicitly add that behavior.

### PostgreSQL development setup

- Provide a Docker Compose `postgres` service using the PostgreSQL 18 image family, a named data volume, and a `pg_isready` health check. Pin the tested image version when implementing the setup.
- Publish its port as `127.0.0.1:5432:5432` when Go runs on the host. Other chat clients only need access to the frontend and Go API ports; they do not connect to port 5432.
- Configure a local database named `chat` and a dedicated `chat_app` login role with ownership/permissions for that application's schema. Do not use the cluster administrator account as the application's connection user. Provision roles/database through the setup process; embedded migrations create application tables within it.
- Keep Compose bootstrap credentials in an ignored root `.env`; keep only placeholder values in `.env.example`. The backend receives the resulting application `DATABASE_URL` as an environment variable.
- The setup guide will use `docker compose up -d postgres`, wait for readiness, set backend environment variables, and then run `go run ./cmd/api` from `apps/api`.
- A native PostgreSQL 18 installation is also supported: create the same role/database and supply the corresponding URL. Docker is a setup convenience, not an application dependency.
- Use `sslmode=disable` only for this local loopback database connection. A remote database should use certificate validation, such as `sslmode=verify-full`, with the appropriate trusted certificate.
- Document that `docker compose down` retains the named database volume, while deleting that volume would delete development data. Do not make destructive volume removal part of ordinary startup or testing.

For the two-computer demonstration, run Go with `HOST=0.0.0.0`, use the server's actual LAN IP in both frontend API and WebSocket URLs, and allow the frontend's exact origin. The browser might load `http://192.168.1.10:3000` and use `http://192.168.1.10:8080` plus `ws://192.168.1.10:8080/ws`. Configure host-only cookies for that host and use test credentials on the local network. `0.0.0.0` is a listen address, not the destination URL clients should enter.

The frontend phase will introduce `NEXT_PUBLIC_API_URL` and `NEXT_PUBLIC_WS_URL`, use credentialed HTTP requests, and handle a `401` or session-expired socket by returning to signin. Production would enable secure cookies and HTTPS/WSS; cloud deployment remains deferred.

## 14. Implementation phases and completion checks

### Phase 1: Foundation and storage

- Confirm the implementation branch and inspect the current checkout before making application edits.
- Pin compatible dependencies; update Go version documentation as needed.
- Add the PostgreSQL Compose service, persistent volume, role/database provisioning, environment examples, and native-installation instructions.
- Add validated configuration, a bounded pgx pool, startup connection checks, migrations, and database-aware health checks.
- Add transaction/query helpers and ensure local credentials, dumps, and caches are ignored.

Complete when a fresh PostgreSQL database initializes correctly, concurrent migration attempts are safe, a second startup preserves data without rerunning migrations, pool/constraint behavior works, and invalid configuration or an unreachable database fails clearly.

### Phase 2: Accounts and sessions

- Implement username/password validation, hashing, signup, login, session lookup, and logout.
- Add cookies, exact-origin HTTP policy, CORS, and auth rate limits.
- Document HTTP examples and error responses needed by signin/signup pages.

Complete when duplicate-name races produce one account, wrong passwords fail, sessions survive restart until expiry, expired/revoked sessions fail, and no response exposes password hashes or usable tokens outside cookies.

### Phase 3: Authenticated realtime connections and presence

- Add upgrade authentication, explicit Origin validation, protocol envelopes, and session registration.
- Add per-user/per-session indexes, reader/writer loops, bounded queues, heartbeat, and cleanup.
- Add `session.ready`, `users.list`, and `presence.changed`.

Complete when unauthenticated upgrades fail, one user can connect twice without duplicate presence, the last disconnect changes presence, and session logout/expiry closes the correct connections.

### Phase 4: Private rooms and reliable message acceptance

- Implement canonical direct rooms and participant membership.
- Add `direct.open`, `rooms.list`, `message.send`, and `room.history`.
- Add transactional sequences, message retry identifiers, acknowledgments, and targeted delivery to all participant devices.

Complete when A-B chat never reaches C, concurrent room opens return one room, message retries create one row, and reconnecting retrieves committed messages missed live.

### Phase 5: Group catalogue and membership

- Add group creation, public catalogue/member listing, self-join, and public membership events.
- Extend message authorization/delivery to group members.
- Enforce history visibility from the member's join boundary.

Complete when a new group contains only its creator, users can discover but cannot read a group before joining, repeated joins do not duplicate membership, and all of a member's devices receive authorized group messages.

### Phase 6: Integration and operational behavior

- Verify ordering, reconnect catch-up, session revocation races, slow clients, database failures, rate limits, and shutdown.
- Run tests with the race detector where supported and verify normal Windows builds.
- Provide a small development client/demo script that uses the actual HTTP login and WebSocket protocol; it is a verification tool, not a replacement frontend.
- Complete backend protocol examples and the two-computer demonstration instructions.

Complete when the backend acceptance tests pass and the frontend contract is stable enough to implement against.

### Phase 7: Frontend handoff

Only after backend completion, implement signup/signin/logout pages, online users, private rooms, group discovery/self-join, and per-room message input/display. Implement reconnect/history reconciliation and multi-device behavior against this contract. R6 and the complete R1 demonstration are verified at this stage, not claimed complete from backend tests alone.

## 15. Test plan

Run integration tests against real PostgreSQL 18 using an explicit `TEST_DATABASE_URL` for a dedicated test database. For each test or isolated suite, create a uniquely named test schema, configure that test's pool with the schema as its search path, apply the same migrations, and drop only that schema during cleanup. The migration version table belongs to that schema. Keep parallel tests isolated and verify transaction behavior using more than one pool connection.

Use a separate test role/database or an ephemeral CI database with permission to create test schemas. Never fall back to `DATABASE_URL` or truncate the development database. Fail integration checks clearly when their test database is unavailable, and document how to provision it. Test the real router and WebSocket handshake with controlled cookies and Origin headers. Prefer service and network behavior tests over tests that copy implementation details.

| Area | Essential scenarios |
| --- | --- |
| PostgreSQL | Fresh/repeated/concurrent migrations; real foreign keys and uniqueness; pool exhaustion deadline; room row-lock ordering; rollback restores room sequence; unavailable database |
| Registration | Valid signup; invalid username/password; case-insensitive duplicates; simultaneous duplicate signup |
| Login/session | Correct/incorrect password; generic invalid-credential response; fresh token; cookie flags; expiry; logout; replacing a browser's old session |
| HTTP boundary | Missing/invalid cookie; forbidden or missing mutation Origin; allowed CORS preflight; credentialed request |
| Socket authentication | No session; expired/revoked session; forbidden/missing Origin; logout racing with registration |
| Presence | Two users see themselves; one user has two connections; one disconnect leaves user online; final disconnect marks offline; heartbeat detects dead peers |
| Private rooms | A-B versus A-C isolation; A-B/B-A concurrency; self-chat rejected; guessed room IDs; account identity spoof attempts |
| Groups | Creator-only membership; global discovery and member listing; self-join only; idempotent join; membership persists offline |
| Group privacy | Nonmember send/history blocked; no content in global events; post-join history boundary; join concurrent with message send |
| Multiple devices | All recipient devices receive; sender devices sync; session-specific logout; another device remains signed in |
| Message persistence | Commit before acknowledgment; transaction failure yields no success/event; no lost committed history after restart |
| Retries/order | Same retry ID deduplicated; changed text/room conflicts; concurrent send sequences; live/history duplicate reconciliation |
| Connection failure | Queue overflow disconnects only that client; read/write error cleanup; malformed JSON; oversized frame; binary input |
| Recovery | Disconnect between commit and acknowledgment; reconnect catches up; fresh device sees authorized history; pagination during live sends |
| Process lifecycle | Active-socket shutdown; pending operation completion/failure; restart preserves durable state but clears presence |

Planned routine checks: `go test ./...`, `go vet ./...`, `go test -race ./...`, and a backend build, with the dedicated PostgreSQL test database configured for integration checks. On this Windows machine the race detector may require selecting the available 64-bit compiler with `CC=C:\msys64\ucrt64\bin\gcc.exe`; normal application builds using pgx do not need a C database driver.

The final manual demonstration uses two accounts on two physical computers and a third test account for proving access isolation. Demonstrate private rooms, creator-only groups, self-join, group messaging, presence, and an additional device/tab for one account. Prepare the required Implementation Explanation slide from the final architecture and protocol, including the client/server separation and why the chosen network protocols are used.

## 16. Backend definition of done

- Authenticated users have stable, unique identities and can use multiple devices.
- Required room and membership rules are enforced on the server for every relevant command.
- Private and group messages reach only authorized users and all of their active connections.
- Public presence and group catalogue features reveal no private message content.
- Saved messages can be recovered after reconnect; failed persistence never produces a success acknowledgment.
- The documented HTTP/WebSocket contracts are implemented and tested.
- Backend tests, race checks, and builds pass on the supported development environment.
- The setup guide is sufficient to connect from another physical computer.
- Frontend work can begin without inventing missing backend contracts.
- Special features remain deferred, and backend completion is distinguished from completing the UI and assignment demonstration.
