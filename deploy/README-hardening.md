# ARTEX — Production Deployment & Hardening Guide

คู่มือ deploy ARTEX ให้ใช้งานจริงแบบปลอดภัย สำหรับทีม security/ops

> **บริบทความเสี่ยง:** ARTEX เป็นแพลตฟอร์ม LLM multi-agent ที่ **รันคำสั่ง recon/exploit จริง** (nmap, curl, สคริปต์, browser) และ **เปิดproxy ดักทราฟฟิกแบบ MITM** ได้ด้วยตัวเอง การวางผิดที่ = เครื่องนี้กลายเป็นจุดโจมตีภายในองค์กร ปฏิบัติกับมันเหมือน **C2 / attack box** ไม่ใช่เว็บแอปทั่วไป

---

## 1. สถาปัตยกรรมที่แนะนำ

```
         Internet / Users
                │  (HTTPS 443)
          ┌─────▼─────┐
          │   Caddy   │  TLS + Basic Auth + IP allowlist + security headers
          └─────┬─────┘
       network: web
          ┌─────▼─────────────────┐
          │        ARTEX          │  :8787 (UI/API)  ·  :8788 (traffic MITM — ไม่ publish)
          └──┬──────────────┬─────┘
   network: backend     network: egress
       (internal)       (ออกเน็ต — คุม firewall)
          │                   │
     ┌────▼────┐         targets / LLM API
     │ Postgres│  (ไม่มี ports, ไม่ออกเน็ต)
     └─────────┘
```

ไฟล์ที่ให้มา:
- `docker-compose.prod.yml` — stack แบบ hardened (แยก network, cap drop, limits, log rotation)
- `.env.prod.example` — ตัวอย่างค่า config/secrets
- `caddy/Caddyfile` — reverse proxy TLS + auth + allowlist

---

## 2. ขั้นตอน deploy

```bash
# 1) เตรียม secrets
cp deploy/.env.prod.example .env
chmod 600 .env
openssl rand -base64 32          # → ใส่ POSTGRES_PASSWORD
docker run --rm caddy caddy hash-password --plaintext 'รหัสแข็งแรงของคุณ'   # → ARTEX_BASICAUTH_HASH

# 2) แก้ .env: ARTEX_TAG (ปักหมุด version), ARTEX_DOMAIN, ARTEX_ALLOW_CIDR, auth

# 3) ชี้ DNS ของ ARTEX_DOMAIN มาที่เครื่องนี้ (Caddy ต้องการเพื่อออก cert)

# 4) start
docker compose -f deploy/docker-compose.prod.yml up -d
docker compose -f deploy/docker-compose.prod.yml ps
docker compose -f deploy/docker-compose.prod.yml logs -f artex
```

เข้าครั้งแรก → `https://ARTEX_DOMAIN/setup` ตั้งรหัส admin (login ภายในใช้ username **`ARTEX`**)

---

## 3. Hardening checklist

### 3.1 Network / exposure (สำคัญที่สุด)
- [ ] **ห้าม publish พอร์ต 8788 ออกไปที่ใด ๆ** — เป็น MITM recording proxy; ใครต่อได้ = ดัก/แก้ทราฟฟิกได้ compose นี้ไม่ publish ให้แล้ว
- [ ] ARTEX publish แค่ `127.0.0.1:8787` (debug บน host) — ทางเข้าจริงผ่าน Caddy เท่านั้น ถ้าไม่ต้องการ debug ให้ลบ `ports:` ของ artex ออกเลย
- [ ] Postgres ไม่มี `ports:` และอยู่ใน network `internal: true` (ออกเน็ตไม่ได้)
- [ ] จำกัด `ARTEX_ALLOW_CIDR` ให้เหลือเฉพาะ office/VPN — อย่าเปิด `0.0.0.0/0`
- [ ] วาง stack ไว้หลัง firewall ที่**คุม egress**: ยอมออกเฉพาะ (ก) scope ที่ได้รับอนุญาตให้ทดสอบ และ (ข) endpoint ของ LLM เท่านั้น ป้องกัน agent ไปแตะของนอกขอบเขต

### 3.2 Authentication
- [ ] ARTEX auth เป็น **single-user (ARTEX + password เดียว), ไม่มี MFA, ไม่มี rate-limit/lockout** → จึง**ต้อง**มี Caddy Basic Auth + IP allowlist เสริมด้านหน้าเสมอ
- [ ] รหัส admin และ Basic Auth ยาว ≥16 ตัว สุ่ม เก็บใน password manager
- [ ] JWT เซ็นด้วย `data/jwt.key` (สร้างตอนรันครั้งแรก) — สำรองและกันอ่าน; ลบ = logout ทุก session

### 3.3 Secrets
- [ ] `.env` `chmod 600`, ไม่ commit (มีใน `.gitignore` แล้ว)
- [ ] **LLM API key: แนะนำตั้งผ่าน UI (System → LLM)** เก็บใน DB แทนการใส่ใน env — เพราะ env อ่านได้จากทุก process ในคอนเทนเนอร์และโผล่ใน `docker inspect`
- [ ] ถ้าจำเป็นต้องใช้ env ให้ใช้ docker/k8s secrets แทน plain `.env`
- [ ] หมุน (rotate) key เป็นระยะ และตั้ง budget/usage alert ฝั่ง LLM provider (agent ยิง token เยอะ)

### 3.4 Container / runtime
- [ ] `no-new-privileges:true`, `cap_drop: ALL` ครบแล้ว; เพิ่มกลับแค่ `NET_RAW` (ให้ nmap SYN scan) — ถ้าไม่ต้อง raw scan ให้ลบออก
- [ ] ปักหมุด `ARTEX_TAG` เป็น version ชัด ๆ แล้ว pull ตรวจ digest; อย่าใช้ `latest` บน prod
- [ ] resource limits (cpu/mem) กัน agent รันหนักจนเครื่องล่ม — ปรับตาม workload
- [ ] วางบน host เฉพาะกิจ (dedicated), อย่าแชร์กับ production service อื่น เพราะเป็น attack box

### 3.5 Data / backup
- [ ] สำรอง Postgres (`pg_dump`) + โฟลเดอร์ `./data` (jwt.key) สม่ำเสมอ
- [ ] schema migrate อัตโนมัติตอน start (idempotent) — backup ก่อน upgrade เสมอ
- [ ] เข้ารหัส volume/disk ที่ระดับ host (ข้อมูลมีผล recon + ผลทดสอบที่อ่อนไหว)

### 3.6 Authorization / legal (เชิงปฏิบัติ ไม่ใช่เทคนิค)
- [ ] ตั้ง **company scope** ให้ตรงกับขอบเขตที่มีเอกสารอนุญาต (RoE/authorization) เท่านั้น — ARTEX จะ scan ตาม scope
- [ ] เปิด **Intercept / human-in-the-loop approval** สำหรับ action ที่กระทบจริง ก่อนเริ่มงานกับ target สำคัญ
- [ ] เก็บ log/report ไว้เป็นหลักฐานการทดสอบตามนโยบายองค์กร

---

## 4. Operations

```bash
# อัปเกรด (ปักหมุด tag ใหม่ใน .env ก่อน)
docker compose -f deploy/docker-compose.prod.yml pull artex
docker compose -f deploy/docker-compose.prod.yml up -d artex   # restart = auto-migrate schema

# backup
docker compose -f deploy/docker-compose.prod.yml exec postgres \
  pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB" > backup-$(date +%F).sql
tar czf data-$(date +%F).tgz data/

# ตรวจสุขภาพ
docker compose -f deploy/docker-compose.prod.yml ps
curl -s http://127.0.0.1:8787/api/health
```

---

## 5. สิ่งที่ compose/README นี้ **ไม่** ครอบคลุม (ทำเพิ่มตามองค์กร)
- WAF / rate-limiting ระดับ L7 (เสริมที่ Caddy/ข้างหน้าได้)
- SSO/OIDC (ARTEX ไม่รองรับ native — ทำที่ reverse proxy เช่น oauth2-proxy)
- Egress firewall rules ตัวจริง (ขึ้นกับ network องค์กร/cloud SG)
- Centralized logging/SIEM (ส่ง json-file logs เข้าระบบกลาง)
- High availability (compose นี้เป็น single-node)
