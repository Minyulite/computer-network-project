# CODEBASE.md — โครงสร้างและ pattern ของ Commonroom

เอกสารนี้อธิบายโครงสร้าง project ปัจจุบัน และ convention การเขียน code ที่ใช้อยู่จริง (สรุปจาก source ณ branch `feat/grammar` หลัง merge `main`) ใช้เป็นคู่มือก่อนเพิ่มหรือแก้ feature

---

## 1. ภาพรวม

**Commonroom** คือแอปแชตแบบ real-time สำหรับวิชา Computer Network (Socket Programming)

| ส่วน | เทคโนโลยี | ที่อยู่ |
|---|---|---|
| Backend | Go 1.26, `net/http`, `github.com/coder/websocket`, `pgx` | `apps/api` |
| Frontend | Next.js 16 (App Router), React 19, TypeScript — **ไม่มี UI/state library เพิ่ม** | `apps/web` |
| Database | PostgreSQL 18 (Docker Compose) | `compose.yaml`, `infra/postgres` |
| AI | Google Gemini ผ่าน HTTPS (แก้ grammar, ตอบแชตบอท) | `apps/api/internal/ai/` |

ความสามารถ: สมัคร/เข้าสู่ระบบ, แชตส่วนตัว (direct), กลุ่ม (สร้าง/เข้าเอง), online presence, หลายอุปกรณ์ต่อบัญชี, reconnect + กู้ข้อความ, แก้ grammar ด้วย AI, สลับภาษาแป้นพิมพ์ไทย↔อังกฤษ (Ctrl+Shift)

### การสื่อสารระหว่างส่วนต่างๆ

```
Browser (Next.js, client-side)
  ├─ HTTP  /auth/*   (signup/login/me/logout, cookie HttpOnly `chat_session`)
  └─ WebSocket /ws   (ข้อความ, ห้อง, presence, history, grammar ทั้งหมด)
          │
     Go API (single process)
        httpapi ─ realtime(Hub) ─ chat(service) ─ store(pgx) ─ PostgreSQL
                      │                 └ auth(service) ┘
                      └ grammar ──HTTPS──► Gemini
```

- **เนื้อหาแชตและ history ทั้งหมดวิ่งผ่าน WebSocket เท่านั้น** HTTP ใช้เฉพาะ auth + `/health`
- Next.js ไม่ได้เป็นตัวกลาง API; browser ติดต่อ Go โดยตรง (`credentials: "include"`) จึงต้องใช้ **hostname เดียวกัน** ระหว่างเว็บ, API, WS (`endpoints()` ใน `lib/api.ts` ตรวจให้)

---

## 2. โครงสร้างโฟลเดอร์

```
.
├── README.md            วิธี setup/รัน/ทดสอบ (หลัก)
├── backend.md           สเปก backend + §7 WebSocket protocol + Grammar correction
├── plan.md              สถานะ acceptance ของแต่ละงาน (T1…T7)
├── compose.yaml         PostgreSQL 18.4 (bind 127.0.0.1:5432, volume chat_postgres_data)
├── infra/postgres/init-chat.sh   สร้าง role chat_app + DB chat, chat_test
├── docs/                เอกสารนี้ (README อ้างถึง demonstration.md / implementation-explanation.html ซึ่งยังไม่อยู่ใน repo)
└── apps/
    ├── api/                          Go backend
    │   ├── cmd/api/main.go           composition root: wiring + graceful shutdown
    │   └── internal/
    │       ├── config/               อ่าน/validate env และ .env
    │       ├── id/                   สร้าง/ตรวจ UUID (ใช้ร่วมทุก package)
    │       ├── store/                SQL ทั้งหมด (pgx), migrations (embed)
    │       ├── auth/                 password (Argon2id), session, rate limit
    │       ├── chat/                 business rule ของห้อง/ข้อความ/บอท
    │       ├── ai/
    │       │   ├── llm/              Gemini client ตัวเดียว (retry + fallback)
    │       │   ├── grammar/          prompt + cleanOutput ของการแก้ grammar
    │       │   └── botai/            ตอบลูกค้าแทนร้าน
    │       ├── httpapi/              HTTP routes (/health, /auth/*) + CORS/Origin
    │       └── realtime/             WebSocket hub (หัวใจของ real-time)
    └── web/                          Next.js frontend
        ├── src/app/                  routes: /, /signin, /signup, layout, globals.css
        ├── src/components/           chat-app, auth-form, grammar-textarea
        ├── src/lib/                  api.ts, word-diff.ts
        ├── src/lib/chat/             socket.ts, store.ts, types.ts, keyboard-language.ts
        ├── e2e/chat.spec.ts          Playwright
        └── scripts/e2e.mjs           runner: start Go+Next บน schema ชั่วคราว
```

---

## 3. Backend (Go)

### 3.1 ชั้น (layering) และทิศทาง dependency

```
cmd/api → httpapi, realtime → chat, auth, ai/grammar, ai/botai → store, ai/llm
                config (อ่านครั้งเดียวใน main) ; id ใช้ได้ทุกชั้น
```

| ชั้น | หน้าที่ | ห้ามทำ |
|---|---|---|
| `store` | SQL, transaction, map error ของ DB → sentinel (`ErrNotFound`, …), ค่า `rooms.kind` (`RoomDirect/Group/Bot`) | ไม่รู้จัก HTTP/WS, ไม่ตัดสิน business rule |
| `auth`, `chat`, `ai/*` | business rule, validate input, แปลง record → type ที่ส่งออก (JSON tag) | ไม่แตะ `http`/`websocket` |
| `httpapi` | decode JSON, set cookie, ตรวจ Origin, แปลง error → HTTP status | ไม่เขียน SQL |
| `realtime` | protocol, connection, route คำสั่ง, broadcast | ไม่เขียน SQL ตรงๆ (เรียกผ่าน `chat`/`auth`) |

**Dependency เป็น interface ฝั่งผู้ใช้** ("accept interfaces, return structs"): `realtime` ประกาศ `Authenticator`, `ChatService`, `GrammarCorrector`, `BotReplier`; `grammar`/`botai` ประกาศ `Generator`; `httpapi` ประกาศ `Pinger` — test ใส่ fake ได้โดยไม่ต้องต่อ DB/AI

`main.go` ประกอบ object ด้วยมือ (ไม่มี DI framework) ใน `run() error` (ให้ `defer` ทำงานครบ): `config.Load → store.Open → store.Migrate → auth.New → llm.New (ตัวเดียวใช้ร่วม) → chat.New → realtime.New(auth, chat, grammar.New(ai), botai.New(ai, chat), origins) → authService.SetRevocationNotifier(hub) → httpapi.NewHandler(httpapi.Deps{…})`. ปิดระบบด้วย `signal.NotifyContext` แล้ว `server.Shutdown` + `hub.Shutdown`

### 3.2 Config (`internal/config`)
- `Load()` อ่าน `.env` ใน working directory (`godotenv`) → `Parse(getenv)` (แยกเพื่อ test ง่าย)
- **env ของ process ชนะไฟล์เสมอ** รวมค่าว่างที่ตั้งไว้ชัดเจน
- error ห้ามมีค่าจริงใน message (parser error อาจมีรหัสผ่าน) — ใช้ข้อความคงที่เช่น `"invalid DATABASE_URL"`
- Required: `DATABASE_URL`, `ALLOWED_ORIGINS`. Optional: `DATABASE_MAX_CONNS`(10), `HOST`(127.0.0.1), `PORT`(8080), `COOKIE_SECURE`, `GEMINI_API_KEY/MODEL/FALLBACK_MODELS`
- ทุกค่า validate ด้วย regexp/parse อย่างเข้มงวด (model name ถูกใส่ใน URL path)

### 3.3 Store (`internal/store`)
- `DB{Pool *pgxpool.Pool}`; ทุก method รับ `parent context.Context` แล้วห่อด้วย `operationContext(parent)` (timeout)
- **ID สร้างที่ application** (UUID) ไม่ใช่ DB default — service สร้างแล้วส่งเข้ามา
- Transaction pattern: `tx, _ := Pool.Begin` → `defer tx.Rollback(context.Background())` → ทำงาน → `Commit`
- Error: คืน sentinel (`ErrNotFound`, `ErrSequenceExhausted`, …) ให้ชั้นบน `errors.Is` ; `mapUserWriteError` แปลง unique violation
- Scan ผ่านฟังก์ชัน `scanXxx(row pgx.Row)` กับ const `messageColumns` เพื่อไม่ให้ column list ซ้ำ
- **Migrations**: ไฟล์ `.sql` ใน `migrations/` ถูก `embed` ; runner บันทึก version+checksum ใน `schema_migrations`, รันใน transaction ภายใต้ advisory lock
  - ❌ ห้ามแก้ migration ที่ apply แล้ว → เพิ่มไฟล์ใหม่ (`002_…sql`)
  - ไฟล์ SQL ต้อง LF (กำหนดใน `.gitattributes`) ไม่งั้น checksum ต่าง OS ไม่ตรง
  - ไม่ใส่ `BEGIN/COMMIT` ในไฟล์ migration (runner เป็นเจ้าของ transaction)
- Schema: `users`, `sessions` (เก็บ **hash ของ token** 32 byte), `rooms` (`kind` = `direct|group`, `last_seq`, `direct_key` generated จากคู่ UUID ที่เรียงแล้ว), `room_members` (`joined_seq`), `messages` (`seq` ต่อห้อง, `client_message_id`) — constraint ใน DB ครอบรูปร่างข้อมูลและช่วง seq ≤ 2^53−1 (ปลอดภัยกับ JS number) ดู ER diagram ใน `store/README.md`

### 3.4 Pattern สำคัญด้านข้อความ
- **ลำดับ + กันซ้ำ**: `SaveMessage` ล็อกแถวห้อง (`SELECT … FOR UPDATE`) → ตรวจ membership → หา retry จาก `(sender_id, client_message_id)` → `last_seq+1` → `INSERT` ใน transaction เดียว ถ้า insert พลาด counter rollback
- **Idempotent retry**: client ส่ง `clientMessageId` เดิมซ้ำได้; server คืน message เดิมพร้อม `created=false` (ไม่ broadcast ซ้ำ) ถ้า text/room ต่างจากเดิม → `ErrIdempotencyConflict`
- **ขอบเขต history ของกลุ่ม**: เข้ากลุ่มแล้วบันทึก `joined_seq = last_seq` ตอนนั้น; history คืนเฉพาะ `seq > joined_seq` (direct เริ่มที่ 0)
- **Direct room**: คู่ผู้ใช้เรียงเป็น `low < high` → `direct_key` unique; เปิดซ้ำคืนห้องเดิม (`created=false`)
- **Pagination**: ใช้ cursor (UUID สำหรับรายการ) และ `beforeSeq/afterSeq/upToSeq` สำหรับ history; `Page[T]{Items, NextCursor}` เป็น generic

### 3.5 Auth (`internal/auth`)
- Password: Argon2id (`password.go`); มี `hashSlots`/`workSlots` จำกัดงาน hash พร้อมกัน และ `dummyHash` กัน timing leak ตอนไม่มี user
- Username: ASCII ที่พิมพ์ได้ 3–24 ตัว มีตัวอักษรอย่างน้อย 1 ตัว (ตัวเลขและสัญลักษณ์ใส่หรือไม่ใส่ก็ได้ เช่น `Mark`, `Mark99`) ; เก็บและแสดงตามที่พิมพ์ แต่ **ซ้ำไม่ได้โดยไม่สนตัวพิมพ์** (unique index บน `lower(username)`) และ sign in ก็ไม่สนตัวพิมพ์ ; กฎอยู่ที่ `auth/username.go`, constraint `users_username_format` ใน `003_english_username.sql`, และ `auth-form.tsx`
- Session: token สุ่ม → เก็บ **SHA-256 digest** ใน DB; cookie `chat_session` (HttpOnly, host-only; `Secure` ตาม `COOKIE_SECURE`); อายุ 7 วัน; token ไม่เคยอยู่ใน JSON
- Login/Signup รับ `oldToken` เพื่อ revoke session เดิมของ browser นี้ (สลับบัญชี)
- `RevocationNotifier` (interface) — `realtime.Hub` implement `SessionRevoked` ; logout **รอ** ให้ hub ปิด socket ของ session นั้นก่อนตอบสำเร็จ
- `rate.go`: rate limit ต่อ IP/username สำหรับ login/signup

### 3.6 HTTP (`internal/httpapi`)
- ใช้ `http.ServeMux` pattern แบบมี method (`"POST /auth/login"`)
- ทุก handler เป็น factory: `func loginHandler(service, cfg) http.HandlerFunc`
- Body: จำกัดขนาด (`authBodyLimit`), ตรวจ UTF-8, `DisallowUnknownFields`, ห้ามมี JSON ต่อท้าย
- Error JSON รูปแบบ `{"error":{"code","message"}}` ผ่าน `writeError`; แปลง service error ใน `authError`
- **Origin ต้องตรงกับ `ALLOWED_ORIGINS`** (CSRF defence) ; POST/OPTIONS ที่ไม่มี Origin ถูกปฏิเสธ ; CORS header ตั้งเฉพาะ origin ที่อนุญาต + `Allow-Credentials`
- `GET /health` ping DB → 200 `{"status":"ok"}` หรือ 503 `{"status":"unavailable"}`

### 3.7 Realtime (`internal/realtime`) — **ส่วนที่ซับซ้อนที่สุด**

**โมเดลคือ single-owner goroutine (actor):** `Hub.run()` เป็น goroutine เดียวที่เป็นเจ้าของ state ทั้งหมด (`connections`, `byUser`, `bySession`, `rates`, `grammarInflight`) จึง **ไม่มี mutex** ใน state เหล่านี้ goroutine อื่นสื่อสารผ่าน channel เท่านั้น:

- `hub.control` — คำสั่งควบคุม (`register`, `remove`, `revoke`, `shutdown`, `grammarDone`) **มี priority เหนือ** `hub.commands`
- `hub.commands` — คำสั่งจาก client (`command{client, envelope}`)

**ต่อ 1 connection มี 3 goroutine** (`client.go`): `readLoop` (อ่าน → parse → ส่งเข้า `hub.commands`), `writeLoop` (อ่านจาก `c.out` → เขียน, timeout 5s), `heartbeat` (ping ทุก 30s + ปิดเมื่อ session หมดอายุ) ตัวใดจบ → ปิดทั้งชุด แล้วส่ง `remove` ให้ hub

**Backpressure:** `c.out` เป็น buffered (64). `hub.enqueue` ใช้ `select … default` ถ้าเต็ม = **ตัด client ที่ช้านั้นทิ้ง** (`"Slow client"`) ไม่บล็อก hub

**Protocol (`protocol.go`)** — JSON text frame เท่านั้น:
```json
{ "v": 1, "type": "message.send", "requestId": "abc", "payload": { … } }
```
- Client→Server: ต้องมี `v=1`, `type`, `requestId` (`^[A-Za-z0-9_-]{1,64}$`), `payload` (object); ขนาด ≤ 16 KiB; ฟิลด์แปลก = ผิด (`DisallowUnknownFields`); envelope เสีย → **ปิด socket**
- Server→Client: ตอบคำสั่งด้วย `request.ok` / `request.error` (+ `requestId` เดิม) ; เหตุการณ์ push ไม่มี requestId: `session.ready`, `presence.changed`, `group.created`, `group.member_joined`, `room.upserted`, `message.created`
- คำสั่ง: `users.list`, `rooms.list`, `groups.list`, `groups.members`, `direct.open`, `group.create`, `group.join`, `message.send`, `room.history`, `grammar.correct`
- response จำกัด 256 KiB — `fitHistory` ตัด history ให้พอดีแล้วตั้ง `hasMore` + cursor ต่อ

**Router (`router.go`)** — ทุกคำสั่งผ่าน `handleCommand` ขั้นตอนเดียวกัน:
1. ตรวจ client ยังอยู่ → 2. `ValidateSessionID` ซ้ำ (session ถูก revoke = เตะออก) → 3. rate limit (`rate.go` token bucket: ทุกคำสั่ง 20/s burst 40 + limit เฉพาะ route: send 5/s burst 10, grammar 0.5/s burst 5) → 4. หา `routes[type]` → `handle[P]` decode payload ด้วย `payloadInto` → เรียก handler → `h.ok`/`h.fail`
- Handler แยกไฟล์ตาม domain: `cmd_rooms.go`, `cmd_messages.go`, `cmd_bots.go`, `grammar.go` ; signature `func (h *Hub) xxx(r request, p xxxRequest) (any, error)`
- Pattern การตอบ: return ค่า → router ส่ง `request.ok` ให้ ; ถ้าต้อง broadcast หลังตอบ (`direct.open`, `group.*`, `message.send`, `bot.*`) เรียก `h.reply(r, …)` → broadcast → `return answered{}, nil` (ลำดับ: ผู้ขอได้ ok ก่อน event เสมอ)
- ชื่อ event อยู่ที่ `events.go` (`eventRoomUpserted`, …)
- **แปลง error → code** รวมศูนย์ที่ตาราง `errorCodes` ใน `errors.go` (`errors.Is` → `{code, message}` คงที่) ❗ error ภายในไม่รั่วออก client; เพิ่ม error ใหม่ต้องเพิ่มแถวที่นี่
- Broadcast: `broadcastAll`, `broadcastUsers(ids…)`; ข้อความส่งถึง `RecipientUserIDs` ที่ `chat` คำนวณ (`EligibleMemberIDs`) — ผู้ใช้ทุกอุปกรณ์ได้รับ
- Limits: 200 connection รวม, 10 ต่อผู้ใช้ (`errCapacity`)

**Grammar (`grammar.go`)** — ตัวอย่าง pattern "งานช้านอก hub": `startGrammar` เพียงเช็คโควตา (in-flight ≤ 3/ผู้ใช้) แล้วเปิด goroutine เรียก AI; ผลส่งกลับผ่าน `control` เป็น `grammarDone` แล้ว hub ตอบใน `finishGrammar` **worker ห้ามแตะ state ของ hub หรือเรียก `h.ok/h.fail`** ; ใช้ `c.ctx` ยกเลิกเมื่อ client หลุด ; **ไม่ log ข้อความผู้ใช้**

**Handler (`handler.go`)** — upgrade WebSocket: ตรวจ Origin ตรง 1 ค่า → อ่านคุกกี้ → `ValidateSession` → `websocket.Accept` → `register` ผ่าน hub → `client.start()` (`InsecureSkipVerify` ที่ Accept ปิดเพราะตรวจ Origin เองแล้ว)

### 3.8 AI (`internal/ai`)
- **`llm`** — Gemini client ตัวเดียวของทั้งระบบ: model หลัก + fallback ตามลำดับ, แยก error ถาวร (`isPermanent`) ออกจากชั่วคราว, `Request.Timeout` (ค่าเริ่มต้น 30s), `JSONSchema` บังคับ output JSON ; sentinel `ErrNotConfigured`, `ErrIncomplete`, `ErrUnavailable`
- **`grammar`** — `Service.Correct(ctx, text, before, after)` เรียก `llm.Generator`: `before/after` เป็น context เท่านั้น ไม่ถูกแก้ ; `buildPrompt` / `cleanOutput` (ลบ quote/whitespace ที่ AI เติม) ; งบเวลา 45s ; error ของ llm ใช้ชื่อเดิม (`grammar.ErrIncomplete` = `llm.ErrIncomplete`)
- **`botai`** — classify (template / in_scope / out_of_scope) แล้วตอบ ; AI ล่ม → ตอบคำขอโทษแทน
- ไม่มี `GEMINI_API_KEY` → ปิด feature ไม่ใช่ mock

### 3.9 Convention การเขียน Go
- Error: sentinel `var ErrXxx = errors.New(…)` ต่อ package; ตรวจด้วย `errors.Is`; message ที่ส่งออกภายนอกเป็นข้อความคงที่
- ทุกฟังก์ชันที่แตะ IO รับ `ctx context.Context` ตัวแรก
- Validation อยู่ใน service (ไม่เชื่อ client): `chat/validate.go` — `callerID`, `parseID` (ผ่าน `id.Canonical`), `cleanText(value, min, max)` (UTF-8 + trim + นับ rune)
- ID ทั้งหมดสร้างด้วย `id.New()` ; idempotency key ที่ต้องคงที่ใช้ `id.FromSeed(seed)`
- Pagination ใน `chat/paging.go`: ดึง `limit+1` แล้ว `paginate(records, limit, key, convert)` ; สิทธิ์เข้าห้องผ่าน `authorizeRoom` ; บันทึกข้อความผ่าน `storeMessage` + `sendResult`
- JSON struct tag เป็น camelCase (`roomId`, `clientMessageId`); เวลาเป็น `time.Time` (RFC 3339)
- Request payload ของ WS เป็น struct เล็กๆ ท้องถิ่นในไฟล์ `cmd_*.go` ของ domain นั้น (`sendRequest`, `historyRequest`, …)
- Module path เป็น placeholder `example.com/computer-network/project/apps/api`

### 3.10 Testing (Go)
- ไฟล์ `*_test.go` ข้างโค้ดเดียวกัน (`package` เดียวกัน)
- Test ที่ใช้ DB อ่าน **`TEST_DATABASE_URL`** เท่านั้น (ห้ามถอยไปใช้ `DATABASE_URL`) ; แต่ละ test สร้าง **schema สุ่ม** ใน `chat_test` แล้วลบทิ้ง (`setup(t)` ใน `chat/service_test.go`)
- มี concurrency test (`TestConcurrentDirectOpenAndPrivacy`, `TestConcurrentJoinSendBoundary`), integration test ของ realtime, unit test ของ config/dotenv, grammar (ไม่เคยเรียก Gemini จริง)
- คำสั่ง: `go test ./...`, `go vet ./...`, `go build ./...`, `go test -race ./...`
- `store/tests/schema.sql` — ตรวจ schema ด้วย `psql` แยกต่างหาก

---

## 4. Frontend (Next.js)

### 4.1 Routes (`src/app`)
| Path | ไฟล์ | หมายเหตุ |
|---|---|---|
| `/` | `page.tsx` → `<ChatApp />` | |
| `/signin`, `/signup` | `<AuthForm mode="…">` | |
| — | `layout.tsx`, `globals.css` | CSS เดียวทั้งแอป (ไม่มี Tailwind/CSS-in-JS) |

ทุก component เป็น **client component** (`"use client"`) ; ไม่มี server component ที่ fetch API

### 4.2 Components (`src/components`)
- **`chat-app.tsx`** — `ChatApp` เรียก `GET /auth/me` : 401 → `router.replace("/signin")`, สำเร็จ → `<ChatWorkspace key={user.id}>` (key = user id เพื่อทำลาย store เมื่อสลับบัญชี). `ChatWorkspace` สร้าง `ChatStore` ครั้งเดียว (`useState(() => new ChatStore(user))`), `store.start()/stop()` ใน `useEffect`, อ่าน state ด้วย `useSyncExternalStore`
  - draft ของแต่ละห้องเก็บใน `drafts: Record<roomId, string>` (state ของ component, หายเมื่อ refresh)
  - `action(fn)` ครอบ async action: set `busy`, จับ error แสดง `actionError`
  - `convertDraft(textarea)` — สลับภาษาแป้นพิมพ์ ใช้ selection หรือทั้ง draft
- **`grammar-textarea.tsx`** — textarea + เมนูคลิกขวา "Correct grammar" → panel สถานะ `menu | loading | preview | nochange | error` (union type `Panel`) ; Accept แทนที่เฉพาะช่วงที่เลือก
  - `requestToken` (ref) เพิ่มทุกครั้งที่ปิด panel เพื่อ **ทิ้งผลของ request ที่ถูกยกเลิก**
  - ถ้า draft เปลี่ยนระหว่างรอ: หา `text` เดิมใน draft ถ้ามี **ที่เดียว** ถึงแทนที่ มิฉะนั้นแจ้ง error
  - `key={room.id}` ที่ call site: การแก้ที่เริ่มในห้องหนึ่งไม่ตกไปห้องอื่น
  - ปุ่มทุกตัวใน form ต้อง `type="button"` (ไม่งั้น submit/ส่งข้อความ)
  - prop `onConvert` (optional) รับ Ctrl+Shift จาก `onKeyDown`
- **`auth-form.tsx`** — ฟอร์ม signin/signup ร่วมกัน เรียก `authRequest`

### 4.3 Lib (`src/lib`)
- **`api.ts`** — `endpoints()` (อ่าน `NEXT_PUBLIC_API_URL`/`NEXT_PUBLIC_WS_URL`, validate), `authRequest<T>(path, body?)` (GET ถ้าไม่มี body, POST ถ้ามี; `credentials:"include"`, timeout 10s), `ApiError(status)`, `errorText(unknown)`
- **`chat/socket.ts`** — `ChatSocket`: WebSocket wrapper
  - `request<T>(type, payload, {timeoutMs})` → Promise ที่จับคู่ด้วย `requestId` (`Map pending`), timeout ค่าเริ่มต้น 10s ข้อความ "No confirmation received…"
  - ตอบ `request.error` → `CommandError(code, message)`
  - reconnect: exponential backoff (500ms→15s + jitter, สูงสุด 8 ครั้งแล้ว `offline`); ก่อน retry เรียก `/auth/me` — 401 = `expired` (ไม่ retry)
  - ตรวจ `session.ready` ว่า user id ตรงกับของเรา ไม่งั้น `expired`
  - `newID()` ใช้ `crypto.getRandomValues` (เพราะ `randomUUID` ใช้ไม่ได้บน HTTP LAN)
- **`chat/store.ts`** — `ChatStore` (state container ของแชต) : ดู §4.4
- **`chat/types.ts`** — type ของ `Room`, `Message`, `Feed`, `ChatState`, `ServerEvent` (discriminated union) ต้องตรงกับ JSON tag ฝั่ง Go
- **`chat/keyboard-language.ts`** — map แป้น Kedmanee↔QWERTY; `convertKeyboardLanguage` เดาทิศทางจากอักขระที่มี
- **`word-diff.ts`** — LCS diff ระดับคำ (whitespace เป็น token ของตัวเอง ต่อกลับได้ตรง) ใช้แสดง grammar suggestion

### 4.4 Pattern: ChatStore (external store แบบ immutable)
- class ธรรมดา + `subscribe/getSnapshot` ให้ `useSyncExternalStore`; **ทุกการเปลี่ยน state ผ่าน `patch(partial)`** สร้าง object ใหม่ (immutable) แล้วแจ้ง listener — ห้าม mutate `this.state`
- **generation counter**: เพิ่มเมื่อ connection เปลี่ยน/หยุด ; async task จับ `const generation = this.generation` แล้วเช็ค `if (generation !== this.generation) return` ก่อนเขียน state — กัน response เก่าเขียนทับหลัง reconnect
- **Task de-duplication**: `historyTasks`, `memberTasks`, `refreshTask` (Map/ตัวแปร promise) — เรียกซ้ำระหว่างทำงาน = คืน promise เดิม
- **Refresh หลัง connect** (`session.ready` → `refresh()`): เริ่ม buffer event (`this.buffered = []`) → โหลด `users.list`, `groups.list`, `rooms.list` ทีละหน้า (`pages()`, limit 50) → เมื่อเสร็จ **replay event ที่ buffer ไว้** ด้วย `applyEvent` — ไม่พลาดเหตุการณ์ที่เกิดระหว่าง sync
- **Contiguous `through`**: `Feed.through` ขยับเฉพาะเมื่อ seq ต่อเนื่อง ไม่ให้ event ใหม่ซ่อนช่องว่าง ; reconnect ใช้ `afterSeq: through` + `upToSeq` ดึงส่วนที่ขาดทีละหน้า
- **ส่งข้อความ** (`send`): สร้าง `PendingMessage` (`sending`) → ตั้ง `clientMessageId` → `message.send`; สำเร็จ = ลบ pending เมื่อ message จริงมาถึง (`acceptMessages` จับคู่ด้วย `clientMessageId`) ; ถ้า ack มาแต่ไม่มี broadcast (retry) ดึง message นั้นด้วย `room.history` ; พลาด/timeout → สถานะ `uncertain` + ปุ่ม Retry ที่ใช้ **ID เดิม** (ไม่ซ้ำเพราะ backend idempotent)
- **Rate limit**: `read()` retry เมื่อได้ `rate_limited` (สูงสุด 3 ครั้ง, หน่วง 250ms×n) ; `pages()` หน่วง 75ms ระหว่างหน้า
- `mergeMessages` : merge ด้วย `id` แล้ว sort ตาม `seq`
- `correctGrammar` ตรวจขีดจำกัดฝั่ง client ก่อน (ข้อความ ≤ 2,000 code point, context ข้างละ 500, frame ≤ 16 KiB) และใช้ timeout 55s (> งบ AI ฝั่ง API 45s)

### 4.5 Convention การเขียน TypeScript/React
- TypeScript strict; path alias `@/` = `src/`
- State ข้อมูลแชต = `ChatStore`; state UI ชั่วคราว = `useState` ใน component
- ไม่มีไลบรารี state/UI เพิ่ม (ใช้ React + CSS ธรรมดา) — dependencies มีแค่ `next`, `react`, `react-dom`
- เขียนแบบกระชับ (หลาย statement ต่อบรรทัดได้) ; คอมเมนต์อธิบาย **เหตุผล/กรณีขอบ** ไม่ใช่ทวนโค้ด
- Accessibility: `aria-live`, `role="log"/"status"/"alert"`, `.sr-only` label
- Error แสดงผ่าน `errorText(e)`; ข้อความ error ที่ผู้ใช้เห็นต้องเป็นภาษาคนอ่านรู้เรื่อง ไม่ใช่ stack/ข้อความภายใน
- Env ฝั่ง browser ต้องขึ้นต้น `NEXT_PUBLIC_` และ **ห้ามใส่ความลับ**

### 4.6 Testing (Frontend)
- `npm run lint`, `npm run typecheck`, `npm run build`
- `npm run test:e2e` → `scripts/e2e.mjs` : ต้องตั้ง `TEST_DATABASE_URL` (ชื่อ DB ต้องมีคำว่า `test`, ห้ามเท่ากับ `DATABASE_URL`) ; สร้าง schema ชั่วคราว, build Go binary, รัน Go (`:18080`) + Next production (`:13000`), เปิด Playwright หลาย browser context ทดสอบ privacy / retry / reconnect / session expiry / Go restart แล้วเก็บกวาด; ตั้ง `GEMINI_API_KEY=""` เสมอ ไม่เรียก Gemini จริง
- หลังรัน e2e ต้อง build ใหม่ด้วย URL ปกติก่อนใช้งานจริง

---

## 5. Security & ความเป็นส่วนตัว (invariants ที่ต้องรักษา)

1. Token session ไม่เคยอยู่ใน JSON/URL ; DB เก็บแค่ digest
2. ทุกคำสั่ง WS ตรวจ session ใหม่ + ตรวจ membership ใน service — **identity มาจาก session ไม่ใช่จาก payload**
3. `Origin` ต้องตรง `ALLOWED_ORIGINS` ทั้ง HTTP mutation และ WS upgrade
4. ผู้ใช้เห็นเฉพาะห้องของตัวเอง ; กลุ่มเห็น history หลังเข้าร่วมเท่านั้น ; ห้ามเพิ่มคนอื่นเข้ากลุ่ม
5. อย่า log เนื้อหาข้อความ/draft/รหัสผ่าน/connection string
6. ข้อความ grammar ส่งออกไป Google — แจ้งผู้ใช้ผ่าน tooltip/README
7. Input ทุกช่องมีขีดจำกัดขนาด (frame 16 KiB, message 2,000 อักษร, response 256 KiB, connection/ผู้ใช้ 10)

---

## 6. Workflow การพัฒนา

- Branch: `feat/*`, `refactor/*` → Pull Request เข้า `main` (merge commit)
- Commit ใช้ conventional style (`feat:`, `refactor:` …)
- Setup เร็ว: copy `.env.example` (root) → `docker compose up -d postgres` ; copy `apps/api/.env.example` → `apps/api/.env` ; `cd apps/api && go run ./cmd/api` ; `cd apps/web && npm ci && npm run dev` → http://localhost:3000
- ตั้ง `DATABASE_URL` ชี้ DB `chat` และ `TEST_DATABASE_URL` ชี้ `chat_test` (แยกกันเสมอ)

---

## 7. Where to add what (คู่มือเพิ่มของใหม่)

### เพิ่มคำสั่ง WebSocket ใหม่ (เช่น `message.delete`)
1. `store/` — เพิ่ม method SQL (+ migration `00N_*.sql` ถ้าเปลี่ยน schema)
2. `chat/service.go` — เพิ่ม method: validate input, ตรวจสิทธิ์, map error; เพิ่ม type/ sentinel ใน `chat/types.go`
3. `realtime/cmd_<domain>.go` — เพิ่ม request struct + method `func (h *Hub) xxx(r request, p xxxRequest) (any, error)` (ถ้าต้อง broadcast: `h.reply` → broadcast → `return answered{}, nil`) ; ลงทะเบียนใน `routes` (`router.go`) ใส่ `limit:` ถ้าต้อง rate limit เฉพาะ ; เพิ่มชื่อ event ใน `events.go` และ method ใน interface `ChatService` (`hub.go`)
4. `realtime/errors.go` — เพิ่มแถว error ใหม่ → code/message ในตาราง `errorCodes`
5. ถ้ามี event push ใหม่: เพิ่มใน `ServerEvent` (`web/src/lib/chat/types.ts`) และ `applyEvent` ใน `store.ts`
6. Frontend: เพิ่ม method ใน `ChatStore` (ใช้ `this.socket.request` / `this.read`, เช็ค `generation`), เรียกจาก `chat-app.tsx`
7. อัปเดต `backend.md` §7, เพิ่ม test (service ด้วย DB จริง, hub/integration)

### เพิ่ม HTTP endpoint
`httpapi/handler.go` ลงทะเบียน route → handler factory ในไฟล์ `*_handlers.go` ; ถ้าอยู่ใต้ `/auth/` จะผ่านการตรวจ Origin/CORS อัตโนมัติ ; นอกนั้นต้องตรวจเอง

### เปลี่ยน schema
เพิ่มไฟล์ใหม่ใน `store/migrations/` (LF, ไม่มี BEGIN/COMMIT, ห้ามแก้ไฟล์เก่า) → ปรับ `scanXxx`/`…Columns` → ปรับ `tests/schema.sql` ถ้าจำเป็น

### เพิ่ม config
เพิ่มฟิลด์ใน `Config` + parse/validate ใน `config.Parse` + `apps/api/.env.example` + test ใน `config_test.go`

### เพิ่มหน้า/ UI
เพิ่ม route ใน `src/app/<path>/page.tsx` (บางๆ ที่ render component) ; component ใหม่ใน `src/components/` ; สไตล์ต่อท้าย `globals.css` ตาม class ที่มีอยู่

### Shop assistants (chatbot) — มีแล้ว
- Schema: `002_bots.sql` (`bots`, `bot_templates`, `users.is_bot`, `rooms.kind='bot'` + `bot_id/bot_customer_id`)
- `store/bots.go` (SQL), `chat/bots.go` (validation, limits 5 บอท/ผู้ใช้, 20 template/บอท, `SendBotReply` idempotent), `ai/llm/` (Gemini client ใช้ร่วมกับ grammar), `ai/botai/` (classify → template/in_scope/out_of_scope), `realtime/bot.go` (worker นอก hub แบบเดียวกับ grammar)
- ทุกบอทมี system user เป็นสมาชิกห้อง bot คู่กับลูกค้า เจ้าของไม่ใช่สมาชิก จึงอ่านแชตไม่ได้โดยโครงสร้าง
- Frontend: `components/bot-manager.tsx`, `ChatStore.saveBot/getBot/openBot`, event `bot.upserted`

### เพิ่มความสามารถ AI อีกตัว
ทำตาม pattern grammar: service แยกใน `internal/ai/<name>` ที่เรียก `llm.Generator` (อย่าเขียน HTTP client ใหม่) + interface เล็กใน `realtime` + `start…`/`finish…` ที่ไม่บล็อก hub (worker → `control` channel → hub ตอบ) + จำกัด in-flight และ rate limit ต่อผู้ใช้

---

## 8. จุดที่ควรระวัง

- Hub ประมวลผลทีละคำสั่ง — **ห้ามทำงานช้า (network, AI) ใน `handleCommand`** ให้ใช้ pattern worker + `grammarDone`
- เพิ่ม field ใน JSON ฝั่ง Go ต้องแก้ type ฝั่ง TS ด้วย (ไม่มี codegen) และ payload จาก client ที่มีฟิลด์เกินจะถูกปฏิเสธ
- Frontend ต้องใช้ hostname เดียวกับ API/WS (cookie เป็น host-only)
- `README.md` อ้างถึง `docs/demonstration.md` และ `docs/implementation-explanation.html` ซึ่งยังไม่มีใน repo ; `apps/api/README.md` เป็น starter เก่า (บางส่วนล้าสมัย เช่น ว่าด้วย `API_BASE_URL`) ใช้ README ราก + `backend.md` เป็นหลัก
- แต่ละ test ที่ใช้ DB เปิด schema ของตัวเอง ไม่ควรเขียน test ที่พึ่งข้อมูลค้างใน `chat_test`
