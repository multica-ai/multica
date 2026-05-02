# Multica Complete Backup

نسخة احتياطية شاملة لمشروع Multica — الكود المصدري + إعدادات النشر + الإصلاحات.

## الفروع

| الفرع | الوصف | آخر تحديث |
|-------|-------|-------------|
| `main` | الكود الأساسي من upstream | 2026-05-02 |
| `fix/daemon-api-timeout-heartbeat` | إصلاحات RTL v3 + daemon timeout | 2026-05-02 |

## الاستعادة السريعة

```bash
# 1. استنساخ المشروع
git clone https://github.com/AnwarPy/multica-complete.git
cd multica-complete

# 2. تثبيت التبعيات
npm install

# 3. تشغيل التطوير
npm run dev

# 4. بناء صورة Docker
docker build -f Dockerfile.web.rtl -t multica-web-rtl:latest .

# 5. تشغيل الكونتِينر
docker run -d --name multica-frontend -p 3000:3000 --network multica-net multica-web-rtl:latest
```

## RTL v3 — ملخص الإصلاحات

| # | الإصلاح | الملف |
|---|---------|-------|
| 1+3 | `lang="ar"` في layout | `apps/web/app/layout.tsx` |
| 2 | Desktop shortcut order-independent | `apps/desktop/src/renderer/index.html` |
| 4 | شيل `overflow-x: hidden` | `apps/web/app/globals.css` |
| 5 | Tracker cleanup | `~/.hermes/scripts/graph_updater.py` |
| 6 | LOCK_SH على القراءة | `~/.hermes/scripts/fact_extractor.py` |

## الملفات المعدّلة (commit f0aa04eb)
- `apps/web/app/layout.tsx`
- `apps/web/app/globals.css`
- `apps/desktop/src/renderer/index.html`

## Docker
- الصورة: `multica-web-rtl:v3.20260502`
- ملف البناء: `Dockerfile.web.rtl` (3 مراحل)
- المنفذ: `3000`
- الشبكة: `multica-net`

## التوثيق الإضافي
- [RTL v3 Critical Fixes](docs/RTL_V3_CRITICAL_FIXES.md)
- [WikiLLM Graph](~/.hermes/graphs/wikillm-integration/MULTICA_RTL_V3_CRITICAL_FIXES_20260502.md)
