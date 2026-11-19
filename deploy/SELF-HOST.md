# ARTEX — คู่มือติดตั้งบนเครื่องส่วนตัว (Self-Host)

รัน ARTEX บนเครื่อง/VM ของตัวเอง สำหรับทีม security ใช้งานเอง
มี 3 วิธีให้เลือกตามความถนัด + ส่วน hardening + วิธีใช้งาน

---

## ภาพรวมสถาปัตยกรรม

```
┌───────────────────────── เครื่อง/VM ของคุณ (dedicated) ─────────────────────────┐
│                                                                                 │
│   เบราว์เซอร์ ──(127.0.0.1:8787)──►  ARTEX  ◄──► PostgreSQL                      │
│                                      │  ▲                                        │
│                     :8788 MITM proxy │  │ อ่าน skills/ , เขียน data/ (jwt.key)   │
│                     (ไม่เปิดออก)      │  │                                        │
│                                      ▼                                           │
│                              nmap / httpx / curl / Playwright                    │
│                                      │                                           │
└──────────────────────────────────────┼──────────────── host firewall (egress) ─┘
                                        │ ยอมออกเฉพาะ: ① LLM API  ② target ในสโคป
                        ┌───────────────┴───────────────┐
                        ▼                               ▼
                 api.anthropic.com              target ที่ได้รับอนุญาต
                (หรือ Gemini/OpenAI)            (ห้ามออกนอกสโคป)
```

หลักการ: ทุกอย่างอยู่ในเครื่องเดียว, UI ผูก `127.0.0.1` เท่านั้น, MITM proxy `:8788` ไม่เปิดออก,
host firewall คุมขาออกให้ agent แตะได้แค่ LLM + target ในสโคป

---

## ก่อนเริ่ม — ข้อควรรู้ด้านความปลอดภัย

- **อย่ารันบนเครื่องใช้งานประจำ** — ARTEX รันคำสั่ง recon/exploit เอง + ถือ LLM key + เป็นเป้าโจมตี
  → รันใน **VM แยก หรือเครื่อง dedicated** ที่ไม่ปนกับไฟล์งาน/SSH key/บัญชีองค์กร
- เครื่องต้องอยู่ใน network ที่**ได้รับอนุญาต**ให้ยิง target และไม่ควรมีทางไปแตะ asset อ่อนไหวที่ไม่เกี่ยว
- ตั้ง **company scope** ให้ตรงกับเอกสารอนุญาต (RoE) เท่านั้น

## ความต้องการระบบ (ขั้นต่ำแนะนำ)

| | ขั้นต่ำ | แนะนำ |
|---|---|---|
| CPU | 2 cores | 4+ cores |
| RAM | 4 GB | 8+ GB (agent + Playwright browser กินเยอะ) |
| Disk | 10 GB | 20+ GB |
| OS | Linux / macOS / Windows (WSL2) | **Linux** (nmap SYN scan เต็มรูปแบบ) |

---

## วิธีที่ 1 — Docker ทั้งหมด (แนะนำสำหรับคนส่วนใหญ่)

ง่ายสุด: ได้ ARTEX + Postgres พร้อมกัน ไม่ต้องลง Go/Node

```bash
git clone https://github.com/<your-fork>/ARTEX.git && cd ARTEX

# ใช้ compose สำหรับ local (bind 127.0.0.1, ไม่มี TLS)
cp deploy/.env.local.example .env
chmod 600 .env
# แก้ POSTGRES_PASSWORD (openssl rand -base64 24)

docker compose -f deploy/docker-compose.local.yml up -d
docker compose -f deploy/docker-compose.local.yml logs -f artex   # ดู log
```

เปิด **http://127.0.0.1:8787** → ครั้งแรกไป `/setup` ตั้งรหัส admin (user คือ `ARTEX`)

> **สคริปต์ช่วยติดตั้ง:** `./install.sh` (เลือกข้อ 1 = All Docker) ก็ได้ผลคล้ายกัน
> แต่ compose ปกติจะ publish `0.0.0.0:8787` → ใช้ `docker-compose.local.yml` ของเราเพื่อ bind แค่ localhost

---

## วิธีที่ 2 — Single binary (compile จาก source, ไม่ต้อง Docker สำหรับ ARTEX)

เหมาะถ้าอยากได้ไฟล์เดียวจบ + คุม flag ได้เต็ม (เช่น bind address) **และจำเป็นถ้าจะใช้ Gemini**
(เพราะต้อง build จาก branch ที่ patch norma แล้ว)

ต้องมี: Go 1.26+, Node 20+/npm, และ PostgreSQL (จะใช้ Docker เฉพาะ DB ก็ได้)

```bash
# 0) PostgreSQL (ถ้ายังไม่มี — ใช้ docker เฉพาะ DB)
docker run -d --name artex-pg -p 127.0.0.1:5432:5432 \
  -e POSTGRES_USER=artex -e POSTGRES_PASSWORD=yourpass -e POSTGRES_DB=artex \
  -v artex-pg:/var/lib/postgresql/data postgres:16-alpine

# 1) build frontend static + ฝังเข้า binary
cd web && npm ci && npm run build:static && cd ..
cp -r web/out server/webui/dist

# 2) compile single binary (ฝัง UI)
CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex

# 3) config DB
cat > config.json <<'JSON'
{ "database": { "host":"127.0.0.1","port":5432,"user":"artex",
  "password":"yourpass","dbname":"artex","sslmode":"disable" },
  "skill_dir":"./skills" }
JSON

# 4) รัน — bind localhost เท่านั้น (สำคัญ!)
./artex -addr 127.0.0.1:8787 -proxy 127.0.0.1:8788
```

> **`./install.sh` (ข้อ 2 = Local)** ทำขั้น 1–3 ให้อัตโนมัติ (ถาม DB, generate config.json, compile)
> แต่รันด้วย `-addr :8787` (ทุก interface) — แนะนำรันเอง/แก้เป็น `127.0.0.1:8787`

---

## วิธีที่ 3 — Dev mode (แก้โค้ด/พัฒนา)

```bash
./dev.sh    # backend :8787 + proxy :8788 + frontend next dev :5173
```
ใช้ตอนพัฒนาเท่านั้น (ไม่ฝัง frontend) ไม่เหมาะใช้งานจริง

---

## เลือกวิธีไหนดี

| สถานการณ์ | วิธี |
|---|---|
| อยากเริ่มเร็วสุด ใช้ Anthropic | **วิธี 1 (Docker)** |
| ต้องใช้ **Gemini** | **วิธี 2** (build จาก branch ที่ patch แล้ว) |
| จะแก้โค้ด/พัฒนา | วิธี 3 |

---

## Hardening บนเครื่องตัวเอง (สำคัญ)

### 1) ผูกเฉพาะ localhost
- Docker: ใช้ `docker-compose.local.yml` (ผูก `127.0.0.1:8787` แล้ว)
- Binary: `-addr 127.0.0.1:8787 -proxy 127.0.0.1:8788`
- **ห้าม**เปิด `:8788` (MITM proxy) ออก LAN เด็ดขาด

### 2) Host firewall คุมขาออก (egress) — ตัวอย่าง Linux ufw
```bash
# ตั้งพื้นฐาน
sudo ufw default deny outgoing
sudo ufw default deny incoming
sudo ufw allow out 53          # DNS
sudo ufw allow out to <LLM_IP> port 443        # LLM API (หรือใช้ชื่อโดเมนผ่าน resolver)
sudo ufw allow out to <TARGET_CIDR>            # target ในสโคปเท่านั้น
# บล็อก cloud metadata เผื่อรันบน VM cloud
sudo ufw deny out to 169.254.169.254
sudo ufw enable
```
> ปรับ `<TARGET_CIDR>` และ LLM ตามจริง — นี่คือหัวใจของการกัน agent ออกนอกสโคป

### 3) อื่น ๆ
- ตั้งรหัส admin ยาว ≥16 ตัว; สำรอง `data/` (มี `jwt.key`) และ postgres volume
- รันใน VM/คอนเทนเนอร์แยกจาก host งานจริง
- LLM key: ใส่ผ่าน UI (System → LLM) เก็บใน DB แทน env

---

## การใช้งาน (quick)

1. `/setup` → ตั้งรหัส admin → login (user `ARTEX`)
2. **System → LLM**: เลือก provider + ใส่ key + Save/Test
   - Anthropic: provider `anthropic`, model เช่น `claude-opus-4-8` — ใช้ release ปกติได้เลย
   - Gemini: provider `openai`, base `https://generativelanguage.googleapis.com/v1beta/openai`,
     model `gemini-3.1-flash-lite` — **ต้องใช้ build จาก branch ที่ patch แล้ว**
3. **Assets/Companies**: สร้าง company + ใส่ scope (domain/ip/cidr) ตาม RoE
4. **Tasks → New**: ตั้ง description + goal, ผูก company scope → รัน
5. ดูผลที่ **Sessions** (การทำงาน agent), **Findings** (ช่องโหว่), **Graph** (แผนผังสำรวจ)
6. เปิด **Intercept** (human-in-the-loop) ถ้าต้องอนุมัติ action ก่อนกับ target สำคัญ

---

## อัปเกรด / สำรอง / ถอน

```bash
# อัปเกรด (Docker)
docker compose -f deploy/docker-compose.local.yml pull artex
docker compose -f deploy/docker-compose.local.yml up -d artex   # restart = auto-migrate schema

# สำรอง
docker compose -f deploy/docker-compose.local.yml exec postgres \
  pg_dump -U artex artex > backup-$(date +%F).sql
tar czf data-$(date +%F).tgz data/

# ถอน (เก็บข้อมูล)
docker compose -f deploy/docker-compose.local.yml down
# ถอน + ลบข้อมูล
docker compose -f deploy/docker-compose.local.yml down -v
```

---

## Troubleshooting

| อาการ | สาเหตุ/วิธีแก้ |
|---|---|
| เปิด :8787 ไม่ได้ | ดู `docker compose ... logs artex`; เช็ค DB healthy; port ชนกับโปรเซสอื่น |
| LLM `invalid x-api-key` / 401 | key ผิด/หมดอายุ/ไม่มี credit |
| Gemini 400 `thought_signature` | ใช้ release ปกติ ไม่ใช่ branch ที่ patch — build จากวิธี 2 |
| 429 rate limit | free tier จำกัด RPM → ตั้ง rate_per_minute ต่อ profile + ลด workers (System settings) |
| nmap scan ช้า/ไม่ครบ | Mac/Win Docker ไม่มี NET_RAW → ได้แค่ TCP connect scan; ใช้ Linux host สำหรับ SYN scan |
| task ค้าง `created` | ดู engine log; เช็คว่า LLM configured + provider ตอบได้ |
