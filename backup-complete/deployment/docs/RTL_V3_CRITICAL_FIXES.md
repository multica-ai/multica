# Multica RTL v3 — Critical Fixes (2026-05-02)

## Overview
ستة إصلاحات حرجة بعد مراجعة Claude Code Opus. صورة Docker `v3.20260502` تعمل على `localhost:3000`.

## RTL Strategy (v3)

| الطبقة | الإعداد |
|--------|---------|
| `<html>` | `lang="ar"` |
| المحتوى | `dir="auto"` (متصفح يكتشف تلقائي) |
| التخطيط | `direction: ltr` أساسي |
| التبديل | `[data-multica-dir] *` بـ `!important` |
| الكود | `direction: ltr !important` دائماً |
| اختصار | Ctrl+Shift (بغض النظر عن ترتيب الضغط) |

## الملفات المعدّلة
1. `apps/web/app/layout.tsx` — `lang="ar"`
2. `apps/web/app/globals.css` — شيل `overflow-x: hidden`
3. `apps/desktop/src/renderer/index.html` — سكريبت toggle مطابق للويب
4. `~/.hermes/scripts/graph_updater.py` — تنظيف tracker
5. `~/.hermes/scripts/fact_extractor.py` — LOCK_SH على القراءة

## النشر
```bash
docker build -f Dockerfile.web.rtl -t multica-web-rtl:v3.20260502 .
docker stop multica-frontend && docker rm multica-frontend
docker run -d --name multica-frontend -p 3000:3000 --network multica-net multica-web-rtl:v3.20260502
```

## Git
- Commit: `f0aa04eb`
- Branch: `fix/daemon-api-timeout-heartbeat`
- Repo: `https://github.com/AnwarPy/multica`

## التقييم
Claude Opus: **8.7/10** (قبل: 7/10)
