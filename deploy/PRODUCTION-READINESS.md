# ARTEX — Production Readiness สำหรับ Cloud Environment

ประเมินโดยมองจากโค้ดจริง (v0.3.7 line) ว่าต้องเติมอะไรบ้างก่อนนำ ARTEX ขึ้นใช้งานจริงบน cloud
จัดลำดับ **P0 (ห้ามข้าม) → P1 (ควรมีก่อนใช้จริงจัง) → P2 (เมื่อขยาย)** พร้อม map กับบริการ cloud

> ARTEX = **attack box ที่รันคำสั่ง recon/exploit จริง + มี MITM proxy ในตัว** ปฏิบัติเหมือน C2
> ไม่ใช่เว็บแอปทั่วไป ทุกข้อด้านล่างมองจากมุมนี้

---

## ⛔ ข้อจำกัดสถาปัตยกรรมที่ต้องรู้ก่อน (สำคัญที่สุด)

1. **รันได้ทีละ 1 instance เท่านั้น (ห้าม scale-out)**
   ตอน start เซิร์ฟเวอร์จะ restore task ที่ค้าง "running" แล้วสั่ง `engine.Run` ต่อ (`server/server.go:268`)
   ถ้ามีหลาย replica → **แต่ละ replica จะรัน task เดียวกันซ้ำ** (ยิง target ซ้ำ, token เปลือง, สภาพ race)
   → ออกแบบเป็น **single replica** (scale **UP** ไม่ใช่ OUT). ต้องการ throughput เพิ่ม = เพิ่ม CPU/mem + workers
2. **Schema migrate ตอน start แบบ idempotent** (รัน `schema.sql` ซ้ำทุกครั้ง)
   → rolling update ที่มี 2 pod พร้อมกันช่วงสั้น ๆ อาจชน migration. ใช้ deploy strategy **Recreate** (ดับเก่าก่อนขึ้นใหม่) ไม่ใช่ RollingUpdate
3. **ไม่เหมาะกับ serverless scale-to-zero** (Cloud Run/Lambda): task รันยาว + WebSocket live stream + engine ถือ state ใน process เดียว
   → ใช้ **VM / ECS service / GKE Deployment (1 replica) ที่รันตลอด**

---

## 🔴 P0 — ปิดช่องก่อนขึ้น prod (security blockers)

| # | สิ่งที่ต้องเติม | ทำไม (จากโค้ดจริง) | วิธีบน cloud |
|---|---|---|---|
| 1 | **Egress firewall + network isolation** | agent รันคำสั่ง recon เอง = ถ้าไม่คุมขาออก มันไปแตะนอก scope / โดน SSRF ชี้ไป metadata endpoint (169.254.169.254) ได้ | วาง subnet แยก, **egress allowlist** เฉพาะ scope ที่ได้รับอนุญาต + endpoint LLM. บล็อก metadata IP. AWS: SG + NACL + NAT/Network Firewall · GCP: VPC egress firewall + Cloud NAT · Azure: NSG + Firewall |
| 2 | **TLS + auth ชั้นหน้า + จำกัดเครือข่าย** | native auth = user `ARTEX` ตัวเดียว, **ไม่มี lockout/rate-limit** (`server/auth.go` ไม่มีนับ fail), JWT 7 วัน, ไม่มี MFA | หน้า ARTEX ด้วย reverse proxy (Caddy ที่ให้ไว้ หรือ ALB/Cloud LB) + **SSO/OIDC via oauth2-proxy หรือ IAP** + IP allowlist/VPN-only + WAF |
| 3 | **Secrets manager** | LLM key/DB creds ถ้าอยู่ `.env`/env จะอ่านได้จากทุก process + `docker inspect` | AWS Secrets Manager · GCP Secret Manager · Azure Key Vault → inject ตอน runtime. อย่า bake ลง image/compose |
| 4 | **แยก MITM proxy :8788 ออกจากทุกเส้นทางเข้า** | ใครต่อ :8788 ได้ = ดัก/แก้ทราฟฟิกได้ | ไม่ publish, ไม่มี LB/Ingress ชี้ไป, NetworkPolicy ปิด (compose.prod ที่ให้ไว้ไม่ publish แล้ว) |
| 5 | **Brute-force protection หน้า login** | ไม่มี lockout ในโค้ด | rate-limit/5xx ที่ edge (Caddy/WAF), fail2ban, หรือ **patch โค้ดเพิ่ม lockout** (ดู P2 #13) |
| 6 | **Metadata/IMDS hardening** | pentest box ที่โดน SSRF แล้วอ่าน IMDS = ขโมย cloud cred ได้ | AWS: **IMDSv2 required** + hop limit 1 · GCP/Azure: บล็อก metadata IP ที่ egress firewall |

---

## 🟠 P1 — ควรมีก่อนใช้งานจริงจัง (reliability / ops)

| # | สิ่งที่ต้องเติม | หมายเหตุ |
|---|---|---|
| 7 | **Managed PostgreSQL** (RDS / Cloud SQL / Azure DB) | แทน container postgres → backup อัตโนมัติ, PITR, HA multi-AZ, encryption-at-rest, patching. ตั้ง `ARTEX_PG_DSN` ชี้ไป endpoint + `sslmode=require` |
| 8 | **Persistent + encrypted storage สำหรับ `./data`** | เก็บ `jwt.key` (ลบ = logout ทุกคน) และ SQLite/ไฟล์งาน → EBS/PD/Managed Disk หรือ EFS/Filestore, เปิด encryption, สำรอง |
| 9 | **Observability** | log ตอนนี้เป็น `log.Printf` plain text → ส่งเข้า CloudWatch/Cloud Logging/Loki. เพิ่ม **metrics** (มีแค่ `/api/health`) ผ่าน sidecar/exporter. alert: 5xx, task fail, engine idle ค้าง |
| 10 | **LLM cost + rate guardrails** | free tier ชน 429 (เห็นในเดโม่). ตั้ง rate_per_minute ต่อ profile, **budget alert** ฝั่ง provider, failover chain (Anthropic + Gemini). Anthropic ไม่ติด thought_signature → ใช้เป็นตัวหลักได้ |
| 11 | **Backup & DR + ทดสอบ restore** | `pg_dump` + snapshot `data/` อัตโนมัติ, retention, **ซ้อม restore จริง**. RTO/RPO ตามนโยบาย |
| 12 | **Image supply-chain** | pin digest (ไม่ใช่ `latest`), **scan ด้วย Trivy/Grype**, สร้าง **SBOM**, **sign ด้วย cosign**, เก็บใน private registry (ECR/Artifact Registry/ACR) |
| 13 | **Health/readiness probes + graceful shutdown** | มี `/api/health` แล้ว → ผูกเป็น liveness/readiness. ตรวจว่า SIGTERM ปิด task/flush สะอาด (สำคัญเพราะ task รันยาว) |
| 14 | **Resource sizing + limits** | agent + browser (Playwright) + nmap กิน CPU/mem สูง → ตั้ง requests/limits สมจริง, เฝ้า OOM |

---

## 🟡 P2 — เมื่อขยาย / ลด risk เพิ่ม

| # | สิ่งที่ควรพิจารณา | หมายเหตุ |
|---|---|---|
| 15 | **Multi-user / RBAC / audit ว่าใครสั่ง scan อะไร** | native เป็น single-user → เริ่มจาก SSO proxy (oauth2-proxy) ที่ edge; ถ้าต้อง RBAC จริงต้อง patch/ขอ feature upstream. audit log มีผลด้าน **RoE/หลักฐานการทดสอบ** |
| 16 | **IaC (Terraform/Pulumi) + CI/CD** | ทำ VPC/SG/Secrets/DB/service ให้ reproducible; pipeline build→scan→sign→deploy (strategy Recreate) |
| 17 | **K8s hardening (ถ้าใช้ K8s)** | NetworkPolicy (เข้าได้แค่จาก ingress, ออกตาม egress policy), PodSecurity restricted เท่าที่ได้ (nmap SYN ต้อง `NET_RAW`), seccomp, read-only rootfs + writable `/app/data` เท่านั้น, 1 replica, strategy Recreate |
| 18 | **เลือก compute ให้ถูก** | VM (ง่าย/คุม network ชัด) · ECS Fargate (ระวัง NET_RAW/SYN scan — Fargate จำกัด; อาจต้อง TCP connect scan) · GKE/EKS (ยืดหยุ่นสุด). **เลี่ยง Cloud Run/Lambda** (task ยาว + WS) |
| 19 | **Session/JWT ปรับปรุง** | TTL 7 วันยาวไปสำหรับ attack box; ลดลง + มีทาง revoke (หมุน `jwt.key`). พิจารณา re-auth สำหรับ action อ่อนไหว |
| 20 | **Human-in-the-loop / Intercept เปิดใช้จริง** | เปิด approval ก่อน action ที่กระทบ target สำคัญ (ARTEX มี intercept อยู่แล้ว) — กัน agent ทำเกินขอบเขต |

---

## สรุปเส้นทางที่แนะนำ (minimal viable prod)

1. **1 VM/ECS/GKE deployment (1 replica)** ใน subnet แยก + **egress allowlist** (scope + LLM เท่านั้น) + บล็อก IMDS
2. **Managed Postgres** (`sslmode=require`) + **Secrets Manager** สำหรับ key/DSN
3. **Caddy/LB TLS + oauth2-proxy (SSO) + IP allowlist** หน้า ARTEX; `:8788` ไม่เปิด
4. **Disk เข้ารหัส** สำหรับ `data/` + **backup อัตโนมัติ** (DB + data) + ซ้อม restore
5. **Logs → central logging + alert** (spend, 5xx, task fail)
6. **Image: pin digest + Trivy scan + cosign sign**; deploy strategy **Recreate**
7. LLM: **Anthropic เป็น primary** (เสถียร, ไม่ติด thought_signature) + Gemini (fork นี้) เป็น failover, มี budget alert

ดูคู่กับ `deploy/README-hardening.md` (รายละเอียด container/network/secrets) และ `deploy/docker-compose.prod.yml` (baseline แบบ single-node)
