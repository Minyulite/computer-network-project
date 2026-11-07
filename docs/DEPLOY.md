# Deploy: backend (Render) + frontend (Vercel) + DB (Neon Postgres)

## ต้องรู้ก่อน (สำคัญ)
- Session cookie เป็น `SameSite=Lax` และ frontend เช็คว่า website กับ API ต้อง **same site**
  → ใช้ `*.vercel.app` คู่ `*.onrender.com` **ไม่ได้** (คนละ site, cookie ไม่ถูกส่ง, WebSocket ไม่ผ่าน auth)
- ต้องมี **custom domain 1 ตัว** แล้วแยก subdomain:
  - `app.<domain>` → Vercel (frontend)
  - `api.<domain>` → Render (backend)
- ไม่มี domain: GitHub Student Developer Pack แจก domain ฟรี 1 ปี (Namecheap .me) หรือซื้อ .xyz/.site ราคาถูก
- Render free: backend หลับหลังไม่มีคนใช้ ~15 นาที (ตื่นช้า ~1 นาที, WebSocket หลุด) → เหมาะ demo
- Neon free: DB ไม่หมดอายุแบบ Render แต่ compute จะ suspend เมื่อว่าง ~5 นาที query แรกหลังตื่นช้า ~1 วินาที
- Backend ต้องรันแค่ **1 instance** (hub เก็บ state ใน memory)

## ไฟล์ที่เตรียมไว้
- `apps/api/Dockerfile`, `apps/api/.dockerignore` — build backend (ทดสอบ build ผ่านแล้ว)
- `render.yaml` — Render Blueprint (เฉพาะ web service; DB อยู่ Neon)
- `apps/api/internal/store/database.go` — ปิด connection ที่ว่าง >4 นาที และให้ startup รอ DB ตื่นได้นานขึ้น (รองรับ Neon suspend)
- `apps/web/src/lib/api.ts` — อนุญาต subdomain ของ domain เดียวกัน (เดิมบังคับ hostname เดียวกัน)

## ขั้นตอน
### 1) Push โค้ด
Merge/push branch นี้ขึ้น GitHub (Render และ Vercel deploy จาก repo)

### 2) Neon: Database
1. https://neon.tech → สร้าง Project (Postgres version 17 หรือ 18 ถ้ามีให้เลือก; region ใกล้ Render เช่น Singapore ถ้า Render ใช้ Singapore)
2. Dashboard → **Connect** → ปิด **Connection pooling** แล้วคัดลอก connection string แบบ direct (host **ไม่มี** `-pooler`)
   - หน้าตา: `postgresql://neondb_owner:xxxx@ep-xxx.<region>.aws.neon.tech/neondb?sslmode=require`
   - ต้องมี `sslmode=require` (Neon บังคับ TLS)
   - ใช้ direct เพราะ backend ใช้ pool ของตัวเองอยู่แล้ว (`DATABASE_MAX_CONNS` ค่าเริ่มต้น 10)
3. ไม่ต้องรัน SQL เอง migration รันตอน backend เริ่ม
4. ถ้า string มี `channel_binding=require` ให้คงไว้ได้ ถ้าเชื่อมต่อไม่ติดให้ลบพารามิเตอร์นั้นออก

### 3) Render: Backend
1. Render → **New → Blueprint** → เลือก repo → ใช้ `render.yaml`
2. กรอก env ที่ `sync: false`:
   - `DATABASE_URL` = connection string จาก Neon
   - `ALLOWED_ORIGINS` = `https://app.<domain>` (ไม่มี `/` ท้าย, ไม่มี path)
   - `GEMINI_API_KEY` = key จาก https://aistudio.google.com/apikey (ไม่ใส่ก็ได้; บอท/grammar จะตอบว่าขัดข้อง)
3. ดู Logs ต้องเห็น `API listening on 0.0.0.0:10000; database ...` (migration รันเอง)
4. เปิด `https://chat-api-xxxx.onrender.com/health` ต้องได้ 200
5. Settings → **Custom Domains** → เพิ่ม `api.<domain>` → Render บอกค่า CNAME

### 4) DNS (ที่ผู้ให้บริการ domain)
- `api` CNAME → ค่าที่ Render ให้ (`chat-api-xxxx.onrender.com`)
- `app` CNAME → `cname.vercel-dns.com` (หรือค่าที่ Vercel แสดงหลังเพิ่ม domain)
- รอ DNS + ออก HTTPS (ปกติไม่กี่นาที)

### 5) Vercel: Frontend
1. Vercel → **Add New Project** → import repo → **Root Directory = `apps/web`** (Framework: Next.js)
2. Environment Variables (ตั้งก่อน build; ค่า `NEXT_PUBLIC_*` ถูกฝังตอน build):
   - `NEXT_PUBLIC_API_URL` = `https://api.<domain>`
   - `NEXT_PUBLIC_WS_URL` = `wss://api.<domain>/ws`
3. Deploy → Settings → Domains → เพิ่ม `app.<domain>`
4. ถ้าเปลี่ยน env ภายหลัง ต้อง **Redeploy**

### 6) ตรวจ
1. เปิด `https://app.<domain>` → สมัครสมาชิก/ล็อกอิน (DevTools → Application → Cookies ต้องมี session cookie ที่ `api.<domain>`)
2. เปิดสองบราวเซอร์/สองบัญชี ส่งข้อความ ดูว่า realtime มา (DevTools → Network → WS ต้องเป็น 101)
3. สร้าง shop bot แล้วลองคุย (ต้องมี `GEMINI_API_KEY`)

## ปัญหาที่พบบ่อย
| อาการ | สาเหตุ |
|---|---|
| "website and API must be on the same site" | env `NEXT_PUBLIC_*` ไม่ใช่ subdomain ของ domain เดียวกับเว็บ |
| Login ได้แต่ refresh แล้วหลุด / WS 401 | ใช้ `*.vercel.app` + `*.onrender.com`, หรือ `COOKIE_SECURE` ไม่ใช่ true |
| `origin_forbidden` | `ALLOWED_ORIGINS` ไม่ตรงกับ origin ของเว็บ (scheme + host เป๊ะ) |
| ครั้งแรกช้า/WS หลุดเป็นช่วงๆ | Render free หลับ — เปิดรอก่อน demo หรือใช้ paid instance |
| Backend start ไม่ขึ้น: "database unavailable" | `DATABASE_URL` ผิด/ไม่มี `sslmode=require`, หรือใช้ host ที่ไม่ใช่ของ project Neon นั้น |
| Backend ไม่ start: invalid HOST/PORT | ปล่อย `PORT` ให้ Render ตั้งเอง และ `HOST=0.0.0.0` |
