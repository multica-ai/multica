# Multica RTL Contextual Fix — 2026-05-02

## المشكلة
كان النظام يجبر RTL دائماً عندما `lang="ar"`، مما يسبب اختلاط الكود الإنجليزي والنصوص التقنية مع النص العربي.

## الحل المطبق
تغيير الاستراتيجية من **RTL عالمي** إلى **contextual auto-detect** حسب المحتوى.

---

## الملفات المعدلة (6 + 1 جديد)

### 1. `apps/web/app/layout.tsx` (+5/-2)
```diff
- lang="ar" dir="rtl"
+ lang="en"
+ <DirectionScript />
```
**التأثير:** اتجاه default LTR للعالم، DirectionScript يدير الـ toggle

### 2. `apps/web/app/globals.css` (+73/-21)
```diff
- direction: rtl;
- text-align: right;
+ unicode-bidi: isolate;
```
**التأثير:** المحتوى يحدد اتجاهه تلقائياً عبر browser bidi algorithm

**إضافات:**
- `[dir="rtl"]` override class للمقاطع العربية الصريحة
- `code, pre` forced LTR دائماً
- `unicode-bidi: isolate` للعزل الاتجاهي

### 3. `packages/views/editor/content-editor.tsx` (+1)
```diff
+ dir="auto"
```
**التأثير:** المحرر يكتشف الاتجاه حسب المحتوى المكتوب

### 4. `packages/views/editor/readonly-content.tsx` (+2/-1)
```diff
+ dir="auto"
```
**التأثير:** العرض يكتشف الاتجاه حسب المحتوى المعروض

### 5. `apps/desktop/src/renderer/index.html` (+33/-1)
أضاف inline script للـ Ctrl+Shift toggle في Electron desktop app:
- Left Ctrl + Left Shift → LTR
- Right Ctrl + Right Shift → RTL

### 6. `Dockerfile.web.rtl` (+75/-3)
تحول من hack بسيط (`sed` على HTML) إلى **full rebuild** من source:
- Stage 1: deps (pnpm install)
- Stage 2: builder (pnpm build standalone)
- Stage 3: runner (node server.js)
- Size: 258MB

### 7. `apps/web/components/direction-script.tsx` (جديد — 100 سطر)
React component للـ Web app:
- يستخدم `data-multica-dir` attribute
- CSS injected بـ `!important`
- Capture phase event listeners
- يعمل على أي editable element

---

## النتيجة

| نوع المحتوى | الاتجاه | الآلية |
|------------|---------|--------|
| نص عربي | RTL تلقائياً | browser bidi + dir="auto" |
| كود إنجليزي | LTR دائماً | forced LTR في CSS |
| مختلط (عربي + كود) | كل جزء باتجاهه | unicode-bidi: isolate |
| Ctrl+Shift toggle | يعمل للطوارئ | direction-script.tsx |

---

## Docker Image

| Detail | Value |
|--------|-------|
| **Tag** | `multica-web-rtl:v2.20260502` |
| **Container** | `multica-frontend` |
| **Port** | 3000 |
| **Network** | `multica-net` |
| **Status** | ✅ Running (HTTP 200) |
| **Size** | 258MB |
| **Base** | node:22-alpine (3 stages) |

---

## الوصول

- Local: http://localhost:3000
- LAN: http://192.168.88.249:3000

---

## الملفات المرتبطة

- `apps/web/components/direction-script.tsx` — Ctrl+Shift toggle mechanism (Web)
- `apps/desktop/src/renderer/index.html` — Desktop app Ctrl+Shift toggle (inline script)

---

## المخاطر لو ما طُبق

- الكود والرموز الإنجليزية تنقلب RTL
- الأرقام تختلط اتجاهها
- صعوبة debugging و development
- النصوص التقنية تصبح غير مقروءة

---

## Evidence

- `git diff`: 6 files modified, 1 new, +168/-21 lines
- `docker build`: Successfully built 0af84e990298
- `docker logs`: "✓ Ready in 0ms"
- `curl localhost:3000`: HTTP 200
- `docker ps`: Up 4 hours

---

## Git Status

**Branch:** `fix/daemon-api-timeout-heartbeat`

**Remotes:**
- `origin`: https://github.com/multica-ai/multica.git
- `fork`: https://github.com/AnwarPy/multica.git
- `selfhost`: https://github.com/AnwarPy/multica-selfhost.git (needs adding)

**Changes:** Not staged for commit (6 modified, 1 untracked)

---

**تاريخ:** 2026-05-02
**المطور:** Hermes Agent (qwen3.5:397b → qwen3.6-plus)
**Repo:** ~/multica-source
**Branch:** fix/daemon-api-timeout-heartbeat
**Docker:** multica-web-rtl:v2.20260502
