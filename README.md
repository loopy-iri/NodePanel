# NodePanel — control plane for selling node access

پنل اصلی (Control Plane) برای **فروش دسترسی نود با سهمیه‌ی حجم**: مدیریت **چند نود (fleet)**، مشتری/پلن/اشتراک/سهمیه، جمع‌آوری مصرف و expose برای بیلینگ بیرونی (ربات فروش)، با **API مستند (OpenAPI)** و **پنل وب ریسپانسیو**.

> Repo: `https://github.com/loopy-iri/NodePanel`
> نودِ چند-مستأجری در ریپوی جدا: `https://github.com/loopy-iri/NodeAgent`

> 📖 **راهنمای کامل (نصب، فروش، اتصال مشتری، API):** [`docs/wiki/`](docs/wiki/Home.md) — نسخه‌ی تب Wiki روی ریپوی NodeAgent منتشر شده است.

## مرز مسئولیت
پنل **منطق پول ندارد** (کیف‌پول/قیمت/پرداخت در ربات فروش است). فقط بایت/زمان/وضعیت را مدیریت و مصرف/overage را از طریق API + webhook expose می‌کند.

## ویژگی‌ها
- مدیریت **چند نود** با master key + TLS (pin گواهی نود یا TOFU خودکار).
- مشتری/پلن/اشتراک: ساخت، provision روی نود (ساخت tenant + کلید یک‌بار)، suspend/resume/topup/renew/delete.
- **Usage collector**: pull مصرف تجمعی از نودها + تجمیع per-customer + بازتاب وضعیت.
- **Webhook با امضای HMAC**: `usage.threshold`, `usage.over_quota`, `subscription.suspended/resumed/expired`.
- **پنل وب**: داشبورد/نودها/مشتری‌ها/پلن‌ها/اشتراک‌ها/وب‌هوک‌ها، ریسپانسیو، RTL، چندتم (light/dark/midnight/emerald)، ویرایشگر کانفیگ هسته‌ی نود.
- **OpenAPI 3** روی `/openapi.yaml` و Swagger UI روی `/docs`.

## اجرا

```bash
# نصب کامل (دانلود باینری per-arch + systemd؛ بدون build روی سرور):
sudo bash -c "$(curl -sL https://raw.githubusercontent.com/loopy-iri/NodePanel/main/scripts/pg-panel.sh)" @ install

# یا از روی clone:
sudo bash scripts/pg-panel.sh install --port 8080
```

دستورهای CLI: `install, update [VER], uninstall, up, down, restart, status, logs, set-token [TOKEN], info, edit-env, completion`.

> نصب از **باینری از‌پیش‌ساخته** (GitHub Releases، web embed‌شده در باینری) و اجرا با **systemd** (سرویس `pg-panel`) — بدون Docker. برای ساخت باینری‌ها یک تگ `v*` push کن.

### اجرای محلی (توسعه)
```bash
$env:PANEL_API_TOKEN="dev-token"; go run ./cmd/panel
# UI:   http://localhost:8080/
# Docs: http://localhost:8080/docs
```

## متغیرهای محیطی

| متغیر | پیش‌فرض | شرح |
|---|---|---|
| `PANEL_HTTP_ADDR` | `:8080` | آدرس گوش‌دادن |
| `PANEL_DB_PATH` | `panel.db` | مسیر SQLite |
| `PANEL_API_TOKEN` | `dev-token-change-me` | توکن Bearer برای `/api/*` |
| `PANEL_COLLECT_INTERVAL` | `30s` | بازه‌ی usage collector |

## ساختار
```
cmd/panel/            entrypoint
internal/store/       SQLite + schema
internal/nodeclient/  client نود (master key + pin/TOFU)
internal/api/         REST API + collector + lifecycle
internal/webhook/     dispatcher امضاشده
internal/web/         پنل وب embed‌شده + openapi.yaml + Swagger UI
```
