# Kế hoạch Việt hóa + Branding Hira cho fork Multica

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Thêm locale `vi` + brand Hira (màu, font, logo, tên sản phẩm) vào fork `saucevn/multica` sao cho mọi lần `git merge upstream/main` về sau gần như không conflict.

**Architecture:** Tận dụng hệ i18n có sẵn của multica (i18next, 25 namespace JSON/locale, parity test) — Việt hóa chỉ là *thêm* thư mục `locales/vi/` và đăng ký locale. Branding làm theo mô hình **overlay**: file CSS override riêng nạp *sau* `tokens.css`, không sửa token gốc. Mọi chỗ buộc phải sửa file upstream được gom thành "touch-point registry" trong `BRANDING.md` để xử lý conflict khi sync.

**Tech Stack:** i18next + react-i18next, Vitest (parity test), Tailwind v4 CSS variables (OKLch/hex), next/font, Go (chi), Electron.

---

## Bối cảnh & nguyên tắc

### Vì sao lần này KHÔNG lặp lại vết xe app-hira

`app-hira` (fork cũ) Việt hóa bằng cách tự chế `packages/views/i18n/vi.ts` (859 dòng, flat key) và sửa thẳng `tokens.css`, viết lại landing/workspace screens → diverge không thể sync. Multica hiện tại **đã có** hạ tầng i18n chuẩn:

- `packages/core/i18n/` — i18next factory, locale adapter (cookie `multica-locale` / localStorage), `pickLocale` (user choice → `navigator.languages` → default).
- `packages/views/locales/{en,zh-Hans,ko,ja}/` — 25 namespace JSON mỗi locale, tổng ≈ 3.300 chuỗi (đã đếm: issues 445, agents 411, onboarding 346, settings 344, autopilots 306, runtimes 295, skills 243, modals 176, …).
- `packages/views/locales/parity.test.ts` — fail nếu locale nào thiếu/thừa key so với `en`. **Đây chính là máy phát hiện "chuỗi mới cần dịch" sau mỗi lần merge upstream.**
- `pnpm typecheck` — mọi map `Record<SupportedLocale, …>` thiếu entry `vi` sẽ fail compile → không thể bỏ sót chỗ wiring.

### Ba lớp thay đổi (quy tắc bất di bất dịch của fork)

| Lớp | Gồm | Rủi ro conflict |
|---|---|---|
| **A. File mới fork sở hữu** | `locales/vi/**`, `packages/ui/styles/brand.css`, `BRANDING.md`, `docs/*.vi.md`, asset logo Hira | **0** — upstream không bao giờ đụng |
| **B. Sửa append-only file upstream** | đăng ký `vi` trong `types.ts`, `index.ts`, `auth.go`, các map `Record<SupportedLocale>`, 1 dòng `@import brand.css` | Rất thấp — diff vài dòng, dễ re-apply |
| **C. Sửa nội dung file upstream** | metadata `layout.tsx`, `email.go`, favicon, electron-builder | Thấp–vừa — **bắt buộc ghi vào touch-point registry** trong `BRANDING.md` |

### Quyết định đã chốt (kế thừa từ app-hira, đã kiểm chứng đúng)

1. **GIỮ NGUYÊN toàn bộ định danh kỹ thuật**: package `@multica/*`, CLI `multica`, Go module `github.com/multica-ai/multica`, tên DB/env `MULTICA_*`, cookie `multica-locale`, tên file `multica-icon.tsx`. Chỉ rebrand bề mặt người dùng thấy. (Đây là lý do duy nhất app-hira còn merge nổi vài tháng đầu.)
2. **GIỮ `DEFAULT_LOCALE = "en"`**. Người dùng VN có `vi` trong `navigator.languages`/`Accept-Language` sẽ tự được match `vi` qua `pickLocale` — không cần đổi default. Đổi default sang `vi` sẽ phá ~6 assertion trong `pick-locale.test.ts` (file upstream) → conflict vĩnh viễn. Nếu sau này vẫn muốn vi-mặc-định, làm ở tầng deploy (set cookie `multica-locale=vi` tại reverse proxy cho first visit) — không sửa code.
3. **Không port landing page và workspace-screen redesign của app-hira** (PR 4–10 trong `rebranding-p4top10.md`) — đó chính là phần làm fork cũ chết. Chỉ port: bản dịch (làm corpus), palette, font, logo, tài liệu.
4. Trong chuỗi `vi/*.json`, tên sản phẩm viết là **"Hira"** (file vi do fork sở hữu, parity test chỉ so key không so value). Chuỗi `en/*` giữ nguyên "Multica" — không sửa.

### Nguồn tư liệu từ app-hira (`/Users/dev/Claude/Developer/Github/app-hira`)

| File | Dùng làm |
|---|---|
| `packages/views/i18n/vi.ts` (859 dòng) | Corpus tham khảo thuật ngữ — **không auto-port** (cấu trúc key đã khác hoàn toàn) |
| `LOCALIZE-REPORT.md` | Glossary EN↔VI chuẩn (status/priority/nav) + thứ tự ưu tiên màn hình |
| `BRAND-GUIDELINE.md` | Palette, typography, logo spec |
| `apps/landing/public/favicon.svg`, `hira-logo.png` | Asset logo |
| `README.vi.md`, `SELF_HOSTING*.vi.md`, `CLI_INSTALL.vi.md`, `CLI_AND_DAEMON.vi.md` | Docs tiếng Việt (sửa URL app.hira.vn nếu đổi domain) |

### Glossary chốt cho bản dịch (nguồn: LOCALIZE-REPORT.md + conventions.mdx)

| EN | VI | Ghi chú |
|---|---|---|
| Issue | công việc | Theo app-hira đã chốt (người dùng app.hira.vn đã quen). UI ngắn có thể giữ "issue" nếu chật chỗ — nhất quán trong cùng màn hình |
| Workspace | workspace | Giữ nguyên (thuật ngữ sản phẩm) |
| Agent / Autopilot / Skill | giữ nguyên | Jargon sản phẩm |
| Inbox | Hộp thư đến | |
| Backlog / Todo / In Progress / In Review / Done / Blocked | Tồn đọng / Cần làm / Đang làm / Đang duyệt / Hoàn tất / Bị chặn | Status enum |
| Urgent / High / Medium / Low | Khẩn cấp / Cao / Trung bình / Thấp | Priority enum |
| Save / Cancel / Create / Delete / Edit | Lưu / Hủy / Tạo / Xóa / Sửa | |
| Settings / Members / Search | Cài đặt / Thành viên / Tìm kiếm | |
| Multica (tên sản phẩm) | Hira | Chỉ trong `vi/*` |

Quy tắc kỹ thuật khi dịch:
- Giữ nguyên placeholder `{{count}}`, `{{name}}`, `{{agent}}`…
- Plural: tiếng Việt không chia số nhiều → chỉ cần key `_other`, bỏ `_one` (giống cách `zh-Hans` đang làm; parity test đã normalize `_one`/`_other` → `_count` nên không fail).
- Khoảng trắng quanh từ tiếng Anh chêm giữa câu Việt: "Tạo issue mới" (1 space thường — tiếng Việt dùng dấu câu nửa rộng như EN).
- Không dịch giá trị enum gửi lên server (`todo`, `in_progress`…) — chỉ dịch label hiển thị.

---

## Task 1: Nhánh làm việc + tài liệu brand + merge driver cho asset

**Files:**
- Create: `BRANDING.md` (repo root)
- Create: `.gitattributes` (repo root — multica chưa có file này; nếu đã có thì append)
- Create: `docs/brand/BRAND-GUIDELINE.md`, `docs/brand/LOCALIZE-REPORT.md` (copy từ app-hira)

- [ ] **Step 1: Tạo nhánh**

```bash
cd /Users/dev/Claude/multica
git fetch upstream
git checkout -b hira/viet-hoa main
```

- [ ] **Step 2: Copy tài liệu brand từ app-hira về làm nguồn chuẩn trong fork**

```bash
mkdir -p docs/brand
cp /Users/dev/Claude/Developer/Github/app-hira/BRAND-GUIDELINE.md docs/brand/
cp /Users/dev/Claude/Developer/Github/app-hira/LOCALIZE-REPORT.md docs/brand/
```

- [ ] **Step 3: Tạo `.gitattributes` + bật merge driver "ours" cho asset brand**

`.gitattributes`:

```gitattributes
# Brand assets — fork-owned; khi merge upstream luôn giữ bản của fork
apps/web/public/favicon.svg merge=ours
apps/desktop/build/icon.png merge=ours
apps/desktop/build/icon.icns merge=ours
apps/desktop/build/icon.ico merge=ours
apps/desktop/resources/icon.png merge=ours
docs/assets/logo-light.svg merge=ours
docs/assets/logo-dark.svg merge=ours
```

```bash
# merge=ours không phải driver mặc định của git — phải khai báo (mỗi máy clone làm 1 lần):
git config merge.ours.driver true
```

- [ ] **Step 4: Tạo `BRANDING.md` khung touch-point registry** (nội dung đầy đủ ở Task 14 — tạo trước với phần "Nguyên tắc 3 lớp" + bảng rỗng để các task sau điền dần)

```markdown
# BRANDING.md — Sổ tay fork Hira

Fork này Việt hóa + rebrand Multica theo nguyên tắc 3 lớp (xem docs/superpowers/plans/2026-06-13-viet-hoa-multica.md).
MỌI lần sửa một file do upstream sở hữu phải thêm 1 dòng vào bảng Touch-point Registry dưới đây.

## Touch-point Registry
| File | Thay đổi | Chính sách khi conflict |
|---|---|---|
<!-- các task sau điền vào đây -->
```

- [ ] **Step 5: Commit**

```bash
git add .gitattributes BRANDING.md docs/brand/
git commit -m "chore(fork): add brand docs, touch-point registry, merge=ours for brand assets"
```

---

## Task 2: Đăng ký locale `vi` trong core i18n (TDD)

**Files:**
- Modify: `packages/core/i18n/types.ts:1-3`
- Modify: `packages/core/i18n/pick-locale.test.ts` (append test, không sửa test cũ)

- [ ] **Step 1: Viết test fail**

Thêm vào cuối describe block hiện có trong `packages/core/i18n/pick-locale.test.ts`:

```ts
it("matches Vietnamese region tags to vi", () => {
  expect(matchLocale(["vi"])).toBe("vi");
  expect(matchLocale(["vi-VN"])).toBe("vi");
  expect(matchLocale(["fr", "vi-VN", "en"])).toBe("vi");
});
```

- [ ] **Step 2: Chạy test xác nhận fail**

```bash
pnpm --filter @multica/core exec vitest run i18n/pick-locale.test.ts
```

Expected: FAIL — `matchLocale(["vi"])` trả về `"en"` (vi chưa nằm trong SUPPORTED_LOCALES).

- [ ] **Step 3: Thêm `vi` vào types.ts**

`packages/core/i18n/types.ts` — sửa 2 dòng đầu (GIỮ `DEFAULT_LOCALE = "en"`):

```ts
export type SupportedLocale = "en" | "zh-Hans" | "ko" | "ja" | "vi";

export const SUPPORTED_LOCALES: SupportedLocale[] = ["en", "zh-Hans", "ko", "ja", "vi"];
export const DEFAULT_LOCALE: SupportedLocale = "en";
```

- [ ] **Step 4: Chạy lại test**

```bash
pnpm --filter @multica/core exec vitest run i18n/pick-locale.test.ts
```

Expected: PASS toàn bộ (test cũ vẫn xanh vì DEFAULT_LOCALE không đổi).

Lưu ý: `pnpm typecheck` lúc này sẽ **fail** ở các map `Record<SupportedLocale, …>` còn thiếu entry `vi` — đó là chủ đích (compiler liệt kê hộ mọi chỗ cần wiring, xử lý ở Task 4–5). Chưa commit vội nếu muốn giữ CI xanh từng commit; hoặc commit gộp cuối Task 5.

- [ ] **Step 5: Ghi registry**

Thêm vào bảng trong `BRANDING.md`:

```
| packages/core/i18n/types.ts | +"vi" vào SupportedLocale + SUPPORTED_LOCALES | Re-apply 2 dòng nếu conflict |
| packages/core/i18n/pick-locale.test.ts | +1 it() block cho vi | Giữ cả hai phía (additive) |
```

---

## Task 3: Server chấp nhận `language = "vi"` (TDD, Go)

**Files:**
- Modify: `server/internal/handler/auth.go:46-51` (map `supportedLanguages`)
- Modify: `server/internal/handler/user_language_test.go` (append test)

- [ ] **Step 1: Viết test fail** — mirror block test `ja` hiện có (dòng 90–102), thêm vào cuối file:

```go
func TestPatchMeLanguageVietnamese(t *testing.T) {
	env := newTestEnv(t) // dùng đúng helper mà các test ja/ko trong file này dùng
	userID := env.userID
	req := newPatchMeRequest(userID, `{"language":"vi"}`)
	resp := env.do(t, req)
	if got, _ := resp["language"].(string); got != "vi" {
		t.Fatalf("expected response language=vi, got %v", resp["language"])
	}
}
```

(Boilerplate setup copy nguyên từ test `TestPatchMe...Japanese` ngay phía trên trong cùng file — giữ đúng helper names của file đó.)

- [ ] **Step 2: Chạy test xác nhận fail**

```bash
cd server && go test ./internal/handler/ -run Vietnamese
```

Expected: FAIL — language bị reject, response giữ giá trị cũ.

- [ ] **Step 3: Thêm `vi` vào map**

`server/internal/handler/auth.go`:

```go
var supportedLanguages = map[string]struct{}{
	"en":      {},
	"zh-Hans": {},
	"ko":      {},
	"ja":      {},
	"vi":      {},
}
```

- [ ] **Step 4: Chạy lại test**

```bash
cd server && go test ./internal/handler/ -run Language
```

Expected: PASS (cả test cũ lẫn mới).

- [ ] **Step 5: Commit + ghi registry**

```bash
git add server/internal/handler/auth.go server/internal/handler/user_language_test.go
git commit -m "feat(i18n): accept vi as user language on server"
```

Registry: `| server/internal/handler/auth.go | +1 dòng "vi" trong supportedLanguages | Re-apply 1 dòng |`

---

## Task 4: Tạo bộ `locales/vi/` + đăng ký RESOURCES

**Files:**
- Create: `packages/views/locales/vi/*.json` (25 file, khởi tạo = copy `en/`)
- Modify: `packages/views/locales/index.ts` (append 25 import + 1 block RESOURCES)

- [ ] **Step 1: Copy 25 file en → vi**

```bash
cp -R packages/views/locales/en packages/views/locales/vi
```

(Khởi tạo bằng nguyên văn EN để parity test xanh ngay; dịch dần ở Task 7–10. Người dùng chọn vi trước khi dịch xong sẽ thấy EN — chấp nhận được trong giai đoạn chuyển tiếp.)

- [ ] **Step 2: Đăng ký trong `packages/views/locales/index.ts`** — thêm sau block import `ja*` (dòng 101):

```ts
import viCommon from "./vi/common.json";
import viAuth from "./vi/auth.json";
import viSettings from "./vi/settings.json";
import viIssues from "./vi/issues.json";
import viAgents from "./vi/agents.json";
import viEditor from "./vi/editor.json";
import viOnboarding from "./vi/onboarding.json";
import viInvite from "./vi/invite.json";
import viLabels from "./vi/labels.json";
import viMembers from "./vi/members.json";
import viMyIssues from "./vi/my-issues.json";
import viSearch from "./vi/search.json";
import viInbox from "./vi/inbox.json";
import viWorkspace from "./vi/workspace.json";
import viProjects from "./vi/projects.json";
import viAutopilots from "./vi/autopilots.json";
import viSkills from "./vi/skills.json";
import viChat from "./vi/chat.json";
import viModals from "./vi/modals.json";
import viRuntimes from "./vi/runtimes.json";
import viLayout from "./vi/layout.json";
import viUsage from "./vi/usage.json";
import viUi from "./vi/ui.json";
import viSquads from "./vi/squads.json";
import viBilling from "./vi/billing.json";
```

và thêm block `vi:` vào cuối object `RESOURCES` (sau block `ja`, giữ đúng thứ tự namespace như block `en` dòng 107–133):

```ts
  vi: {
    common: viCommon,
    auth: viAuth,
    settings: viSettings,
    issues: viIssues,
    agents: viAgents,
    editor: viEditor,
    onboarding: viOnboarding,
    invite: viInvite,
    labels: viLabels,
    members: viMembers,
    "my-issues": viMyIssues,
    search: viSearch,
    inbox: viInbox,
    workspace: viWorkspace,
    projects: viProjects,
    autopilots: viAutopilots,
    skills: viSkills,
    chat: viChat,
    modals: viModals,
    runtimes: viRuntimes,
    layout: viLayout,
    usage: viUsage,
    ui: viUi,
    squads: viSquads,
    billing: viBilling,
  },
```

- [ ] **Step 3: Chạy parity test**

```bash
pnpm --filter @multica/views exec vitest run locales/parity.test.ts
```

Expected: PASS — vi có đủ 25 namespace, đủ key (đang là bản copy EN).

- [ ] **Step 4: Commit + ghi registry**

```bash
git add packages/views/locales/vi packages/views/locales/index.ts
git commit -m "feat(i18n): add vi locale bundle (bootstrap from en)"
```

Registry: `| packages/views/locales/index.ts | +25 import vi* + block vi trong RESOURCES | Append-only, re-apply block nếu conflict |`

---

## Task 5: Wiring UI — language switcher + các map `Record<SupportedLocale>`

**Files:**
- Modify: `packages/views/settings/components/preferences-tab.tsx:121-126`
- Modify: `packages/views/locales/{en,zh-Hans,ko,ja,vi}/settings.json` (+1 key `vietnamese`)
- Modify: `apps/web/app/layout.tsx:97-102` (HTML_LANG)
- Modify: `apps/desktop/src/renderer/src/App.tsx:29-…` (HTML_LANG)
- Modify: `packages/views/onboarding/templates/index.ts:33-38` (CONTENT_LANG_BY_LOCALE)
- Modify: `apps/web/lib/use-cases-i18n.ts:16-…` (useCaseText)

- [ ] **Step 1: Thêm key label ngôn ngữ vào settings.json của CẢ 5 locale**

Theo đúng pattern hiện có (label là tên bản ngữ, giống `"korean": "한국어"` trong file en) — thêm vào object `preferences.language` của **mỗi** file `packages/views/locales/{en,zh-Hans,ko,ja,vi}/settings.json`:

```json
"vietnamese": "Tiếng Việt"
```

(Cùng một giá trị "Tiếng Việt" cho cả 5 file — tên bản ngữ không dịch.)

- [ ] **Step 2: Thêm option vào `preferences-tab.tsx`** (sau dòng 125):

```ts
const languageOptions: { value: SupportedLocale; label: string }[] = [
  { value: "en", label: t(($) => $.preferences.language.english) },
  { value: "zh-Hans", label: t(($) => $.preferences.language.chinese) },
  { value: "ko", label: t(($) => $.preferences.language.korean) },
  { value: "ja", label: t(($) => $.preferences.language.japanese) },
  { value: "vi", label: t(($) => $.preferences.language.vietnamese) },
];
```

- [ ] **Step 3: HTML_LANG web** — `apps/web/app/layout.tsx`:

```ts
const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
  ko: "ko-KR",
  ja: "ja-JP",
  vi: "vi-VN",
};
```

- [ ] **Step 4: HTML_LANG desktop** — `apps/desktop/src/renderer/src/App.tsx`, thêm `vi: "vi-VN",` y hệt.

- [ ] **Step 5: Onboarding content fallback** — `packages/views/onboarding/templates/index.ts`:

```ts
const CONTENT_LANG_BY_LOCALE: Record<SupportedLocale, ContentLang> = {
  en: "en",
  "zh-Hans": "zh",
  ko: "ko",
  ja: "ja",
  vi: "en", // chưa có onboarding content tiếng Việt — fallback EN
};
```

- [ ] **Step 6: Use-case pages** — `apps/web/lib/use-cases-i18n.ts`, thêm entry `vi` (dịch sẵn, dùng "Hira"):

```ts
vi: {
  indexTitle: "Tình huống sử dụng",
  indexSubtitle:
    "Xem cách các đội nhóm tổ chức con người và agent làm việc cùng nhau với Hira.",
  indexMetadataTitle: "Tình huống sử dụng",
  indexMetadataDescription:
    "Xem cách các đội nhóm đưa con người và agent vào làm việc cùng nhau với Hira.",
  cardReadMore: "Đọc tiếp →",
  tableOfContents: "Trong trang này",
},
```

- [ ] **Step 7: Typecheck + test toàn bộ**

```bash
pnpm typecheck && pnpm test
```

Expected: PASS. Nếu typecheck còn báo map nào thiếu `vi` mà plan chưa liệt kê (upstream thêm sau này) — thêm entry tương tự rồi ghi vào registry.

- [ ] **Step 8: Smoke test thủ công**

```bash
make dev
```

Mở Settings → Preferences → chọn "Tiếng Việt" → app reload, `<html lang="vi-VN">`, UI hiển thị (tạm thời vẫn chữ EN vì chưa dịch). Kiểm tra DB: cột `user.language = 'vi'`.

- [ ] **Step 9: Commit + ghi registry (5 dòng cho 6 file trên)**

```bash
git add -A && git commit -m "feat(i18n): wire vi locale into language switcher and locale maps"
```

---

## Task 6: Font hỗ trợ dấu tiếng Việt

**Files:**
- Modify: `apps/web/app/layout.tsx:22-25,39-51`

Lý do: `Inter` và `Source_Serif_4` đang load `subsets: ["latin"]` — thiếu glyph dấu tiếng Việt → chữ có dấu sẽ rơi xuống font fallback (xấu, lệch baseline). Cả hai font đều có subset `vietnamese` trên Google Fonts. `Geist_Mono` là font code — không cần sửa (hành vi giống chuỗi CJK trong code block, fallback hệ thống xử lý).

- [ ] **Step 1: Sửa subsets**

```ts
const inter = Inter({
  subsets: ["latin", "vietnamese"],
  variable: "--font-inter",
});
```

```ts
const sourceSerif = Source_Serif_4({
  subsets: ["latin", "vietnamese"],
  style: ["normal", "italic"],
  variable: "--font-serif",
  fallback: [ /* giữ nguyên */ ],
});
```

- [ ] **Step 2: Build kiểm chứng**

```bash
pnpm --filter @multica/web build
```

Expected: build xanh (nếu next/font báo subset không tồn tại → font đó không có subset vietnamese, bỏ qua và ghi chú lại — nhưng Inter/Source Serif 4 chắc chắn có).

- [ ] **Step 3: Commit + ghi registry**

```bash
git add apps/web/app/layout.tsx
git commit -m "feat(i18n): load vietnamese font subsets for Inter and Source Serif"
```

---

## Task 7: Dịch đợt 1 — khung app (layout, common, ui, settings, auth)

**Files:**
- Modify: `packages/views/locales/vi/{layout,common,ui,settings,auth}.json` (≈ 470 chuỗi)

Quy trình dịch chung cho Task 7–10 (lặp lại mỗi đợt):

- [ ] **Step 1: Dịch giá trị** trong từng file vi/*.json theo Glossary ở đầu plan. Đối chiếu corpus cũ để giữ giọng văn nhất quán: `/Users/dev/Claude/Developer/Github/app-hira/packages/views/i18n/vi.ts` và `docs/brand/LOCALIZE-REPORT.md`. Chỉ sửa **value**, không sửa key. Plural: gộp `_one`/`_other` → giữ một key `_other`. Ví dụ chuyển đổi thực tế:

```json
// en/common.json                      →  vi/common.json
"save": "Save",                            "save": "Lưu",
"cancel": "Cancel",                        "cancel": "Hủy",
"delete": "Delete",                        "delete": "Xóa",

// en/settings.json                    →  vi/settings.json
"title": "Settings",                       "title": "Cài đặt",

// plural — en có _one/_other:
"issue_count_one": "{{count}} issue",
"issue_count_other": "{{count}} issues",
// vi chỉ cần:
"issue_count_other": "{{count}} công việc",
```

- [ ] **Step 2: Chạy parity test** (bắt lỗi xóa nhầm key / sót placeholder):

```bash
pnpm --filter @multica/views exec vitest run locales/parity.test.ts
```

Expected: PASS.

- [ ] **Step 3: Soát placeholder bằng grep** — số lượng `{{` mỗi file vi phải bằng file en tương ứng:

```bash
for f in layout common ui settings auth; do
  echo "$f: en=$(grep -o '{{' packages/views/locales/en/$f.json | wc -l) vi=$(grep -o '{{' packages/views/locales/vi/$f.json | wc -l)";
done
```

- [ ] **Step 4: Smoke test UI** — `make dev`, chọn Tiếng Việt, duyệt sidebar/topbar/settings, soát tràn chữ (tiếng Việt dài hơn EN ~20–30%, chú ý nút và tab hẹp).

- [ ] **Step 5: Commit**

```bash
git add packages/views/locales/vi
git commit -m "feat(i18n): translate app shell namespaces to Vietnamese"
```

---

## Task 8: Dịch đợt 2 — luồng issue (issues, inbox, my-issues, search, labels, members, workspace, projects)

**Files:**
- Modify: `packages/views/locales/vi/{issues,inbox,my-issues,search,labels,members,workspace,projects}.json` (≈ 740 chuỗi, issues.json 445 là file lớn nhất)

- [ ] **Step 1: Dịch** — như quy trình Task 7. Enum status/priority dùng đúng bảng glossary (Tồn đọng / Cần làm / Đang làm / Đang duyệt / Hoàn tất / Bị chặn; Khẩn cấp / Cao / Trung bình / Thấp).
- [ ] **Step 2: Parity test** — như Task 7 Step 2. Expected: PASS.
- [ ] **Step 3: Soát placeholder** — script như Task 7 Step 3 với danh sách file của đợt này.
- [ ] **Step 4: Smoke test** — board kanban, issue detail, bộ lọc, search.
- [ ] **Step 5: Commit** — `git commit -m "feat(i18n): translate issue-flow namespaces to Vietnamese"`

---

## Task 9: Dịch đợt 3 — agent (agents, autopilots, chat, skills, runtimes)

**Files:**
- Modify: `packages/views/locales/vi/{agents,autopilots,chat,skills,runtimes}.json` (≈ 1.360 chuỗi — đợt nặng nhất)

- [ ] **Step 1: Dịch** — autopilots.json tham khảo trực tiếp template prompts tiếng Việt rất tốt trong corpus app-hira (`vi.ts` phần `autopilots.template.*`). Giữ nguyên "Agent", "Autopilot", "Skill".
- [ ] **Step 2: Parity test.** Expected: PASS.
- [ ] **Step 3: Soát placeholder** (đặc biệt `{{agent}}` trong chat.json).
- [ ] **Step 4: Smoke test** — trang Agents, Autopilots, panel Chat.
- [ ] **Step 5: Commit** — `git commit -m "feat(i18n): translate agent namespaces to Vietnamese"`

---

## Task 10: Dịch đợt 4 — phần còn lại (onboarding, modals, editor, invite, usage, squads, billing)

**Files:**
- Modify: `packages/views/locales/vi/{onboarding,modals,editor,invite,usage,squads,billing}.json` (≈ 790 chuỗi)

- [ ] **Step 1: Dịch** — onboarding.json chứa "Welcome to Multica!" → "Chào mừng đến với Hira!" (mọi chỗ nhắc tên sản phẩm trong vi đều là Hira).
- [ ] **Step 2: Parity test.** Expected: PASS.
- [ ] **Step 3: Soát placeholder.**
- [ ] **Step 4: Chạy full** — `pnpm test && pnpm typecheck`. Expected: PASS toàn bộ.
- [ ] **Step 5: Commit** — `git commit -m "feat(i18n): complete Vietnamese translation for all namespaces"`

---

## Task 11: Brand overlay — palette Hira (KHÔNG sửa tokens.css)

**Files:**
- Create: `packages/ui/styles/brand.css`
- Modify: `apps/web/app/globals.css:5` (+1 dòng import)
- Modify: `apps/desktop/src/renderer/src/globals.css:5` (+1 dòng import)

- [ ] **Step 1: Tạo `packages/ui/styles/brand.css`** — map palette Hira (từ `docs/brand/BRAND-GUIDELINE.md`) lên đúng các semantic token mà `tokens.css` định nghĩa. File nạp SAU tokens.css nên thắng cascade; upstream đổi token mới → fork tự nhận, chỉ token bị override mới giữ màu Hira:

```css
/* Hira brand overlay — fork-owned. Loaded AFTER tokens.css in both apps.
   NEVER edit packages/ui/styles/tokens.css (upstream-owned).
   Palette source: docs/brand/BRAND-GUIDELINE.md (Electric Indigo / Signal Amber). */

:root {
    /* Electric Indigo #4F46E5, Deep Indigo #3730A3, Indigo Mist #EEF2FF */
    --primary: #4F46E5;
    --primary-foreground: #FFFFFF;
    --brand: #4F46E5;
    --brand-foreground: #FFFFFF;
    --ring: #A5B4FC;
    --warning: #F59E0B; /* Signal Amber */
    --sidebar-primary: #4F46E5;
    --sidebar-primary-foreground: #FFFFFF;
    --sidebar-accent: #EEF2FF;
    --sidebar-accent-foreground: #3730A3;
    --sidebar-ring: #A5B4FC;
    /* Chart ramp: indigo đậm → nhạt (giữ logic primary→tertiary của upstream) */
    --chart-1: #4F46E5;
    --chart-2: #6366F1;
    --chart-3: #818CF8;
    --chart-4: #A5B4FC;
    --chart-5: #C7D2FE;
}

.dark {
    --primary: #6366F1;
    --primary-foreground: #FFFFFF;
    --brand: #6366F1;
    --brand-foreground: #FFFFFF;
    --ring: #4338CA;
    --warning: #FBBF24;
    --sidebar-primary: #6366F1;
    --sidebar-primary-foreground: #FFFFFF;
    --sidebar-accent: #312E81; /* indigo-900 — active state trên nền tối */
    --sidebar-accent-foreground: #E0E7FF;
    --sidebar-ring: #4338CA;
    --chart-1: #818CF8;
    --chart-2: #6366F1;
    --chart-3: #4F46E5;
    --chart-4: #4338CA;
    --chart-5: #3730A3;
}
```

Chủ đích KHÔNG override: `--background/--foreground/--muted/--accent/--secondary/--border` (giữ nền zinc trung tính của upstream — đỡ rủi ro contrast; chỉnh thêm sau nếu muốn "Paper #F8FAFC" của Hira).

- [ ] **Step 2: Import vào web** — `apps/web/app/globals.css`, thêm 1 dòng ngay sau import base.css (dòng 5):

```css
@import "../../../packages/ui/styles/brand.css";
```

- [ ] **Step 3: Import vào desktop** — `apps/desktop/src/renderer/src/globals.css`, sau dòng 5:

```css
@import "@multica/ui/styles/brand.css";
```

- [ ] **Step 4: Kiểm chứng bằng mắt** — `make dev`: nút primary, link, active sidebar, focus ring phải ra indigo; toggle dark mode kiểm tra lại. Chart trang Usage ra dải indigo.

- [ ] **Step 5: Commit + ghi registry**

```bash
git add packages/ui/styles/brand.css apps/web/app/globals.css apps/desktop/src/renderer/src/globals.css
git commit -m "feat(brand): add Hira palette overlay on top of upstream tokens"
```

Registry: `| apps/web/app/globals.css | +1 @import brand.css | Re-apply 1 dòng |` (+ dòng tương tự cho desktop globals.css)

---

## Task 12: Logo, favicon, metadata web

**Files:**
- Modify (replace nội dung): `apps/web/public/favicon.svg`
- Create: `apps/web/public/hira-logo.png`
- Modify: `apps/web/app/layout.tsx:62-91` (metadata) và `:53-60` (viewport themeColor)

- [ ] **Step 1: Copy asset Hira**

```bash
cp /Users/dev/Claude/Developer/Github/app-hira/apps/landing/public/favicon.svg apps/web/public/favicon.svg
cp /Users/dev/Claude/Developer/Github/app-hira/apps/landing/public/hira-logo.png apps/web/public/hira-logo.png
```

(favicon.svg đã nằm trong `.gitattributes merge=ours` từ Task 1.)

- [ ] **Step 2: Sửa metadata trong `apps/web/app/layout.tsx`**

```ts
export const metadata: Metadata = {
  metadataBase: new URL("https://app.hira.vn"),
  title: {
    default: "Hira — Quản lý công việc cho đội ngũ Người + AI",
    template: "%s | Hira",
  },
  description:
    "Nền tảng biến coding agent thành đồng đội thực thụ. Giao việc, theo dõi tiến độ, tích lũy kỹ năng.",
  icons: {
    icon: [{ url: "/favicon.svg", type: "image/svg+xml" }],
    shortcut: ["/favicon.svg"],
  },
  openGraph: {
    type: "website",
    siteName: "Hira",
    locale: "vi_VN",
  },
  alternates: {
    canonical: "/",
  },
  robots: {
    index: true,
    follow: true,
  },
};
```

(Bỏ hẳn block `twitter` — fork không có handle; hoặc thay bằng handle thật nếu có.)

- [ ] **Step 3: Build + soi tab trình duyệt** — `pnpm --filter @multica/web build` xanh; chạy dev thấy favicon Hira + title "Hira — …".

- [ ] **Step 4: Tìm các chỗ render logo Multica trong UI** để quyết định thay:

```bash
grep -rn "MulticaIcon\|multica-icon" packages apps --include="*.tsx" -l
```

`packages/ui/components/common/multica-icon.tsx` là brand mark (asterisk 8 cánh) dùng trong app shell. Chính sách sync-friendly: **giữ nguyên tên file/export `MulticaIcon`**, chỉ thay phần SVG/clip-path bên trong bằng logomark "h." của Hira (1 file, ghi registry). KHÔNG đổi tên symbol (mọi call-site giữ nguyên → không conflict).

- [ ] **Step 5: Commit + ghi registry**

```bash
git add -A && git commit -m "feat(brand): Hira favicon, logo, web metadata"
```

Registry: `| apps/web/app/layout.tsx | metadata + themeColor + HTML_LANG + font subsets | File hay đổi upstream — khi conflict: lấy bản upstream rồi re-apply 3 vùng theo BRANDING.md |`, `| packages/ui/components/common/multica-icon.tsx | thay SVG bên trong, GIỮ tên export | Re-apply nội dung SVG |`

---

## Task 13: Email server tiếng Việt + sender Hira

**Files:**
- Modify: `server/internal/service/email.go:41,155,170`

- [ ] **Step 1: Sửa 3 chuỗi**

```go
// dòng 41 — sender (domain phải được verify ở email provider trước khi deploy):
from := "noreply@hira.vn"

// dòng 155 — subject mã xác thực:
Subject: "Mã xác thực Hira của bạn",

// dòng 170 — subject lời mời:
Subject: fmt.Sprintf("%s đã mời bạn vào %s trên Hira", inviterName, workspaceName),
```

(Giữ đúng tên biến hiện có tại từng dòng — mở file xem context trước khi sửa; nếu body template HTML cùng file có chữ "Multica"/EN thì dịch luôn trong cùng commit.)

- [ ] **Step 2: Chạy Go test**

```bash
cd server && go test ./internal/service/ ./internal/handler/
```

Expected: PASS (nếu có test assert subject cũ → sửa expectation trong cùng commit, ghi chú vào registry).

- [ ] **Step 3: Commit + ghi registry**

```bash
git add server/internal/service/email.go
git commit -m "feat(brand): Vietnamese email subjects and Hira sender"
```

---

## Task 14: Desktop branding (làm khi có ship desktop; bỏ qua nếu chỉ chạy web)

**Files:**
- Modify: `apps/desktop/package.json:3,6,14-15`
- Modify: `apps/desktop/electron-builder.yml:1-2,12-14,26,33,48,64-66,78,90-100`
- Modify: `apps/desktop/src/main/index.ts:287,296`
- Replace: `apps/desktop/build/icon.{png,icns,ico}`, `apps/desktop/resources/icon.png`

- [ ] **Step 1:** `package.json`: `"productName": "Hira"`, description, `"email": "support@hira.vn"`.
- [ ] **Step 2:** `electron-builder.yml`: `appId: vn.hira.desktop`, `productName: Hira`, protocol `hira`, `StartupWMClass: Hira`, artifact pattern `hira-desktop-…`, block `publish` trỏ về `owner: saucevn, repo: multica` (auto-update lấy release từ fork, KHÔNG phải upstream).
- [ ] **Step 3:** `src/main/index.ts`: `app.setName("Hira")` (cả 2 nhánh dev/prod nếu có).
- [ ] **Step 4:** Generate icon từ `hira-logo.png` 1024×1024 (`icns`/`ico` bằng `iconutil`/electron-icon-builder) và thay 4 file build/resources.
- [ ] **Step 5:** `pnpm --filter @multica/desktop build && pnpm --filter @multica/desktop package` — mở .app kiểm tên/icon/menu bar.
- [ ] **Step 6:** Commit + ghi registry (3 file config trên đều là touch-point).

---

## Task 15: Docs tiếng Việt + glossary

**Files:**
- Create: `README.vi.md`, `SELF_HOSTING.vi.md`, `SELF_HOSTING_ADVANCED.vi.md`, `SELF_HOSTING_AI.vi.md`, `CLI_INSTALL.vi.md`, `CLI_AND_DAEMON.vi.md` (copy từ app-hira, sửa URL/tên nếu lệch)
- Create: `apps/docs/content/docs/developers/conventions.vi.mdx` (glossary VI — file MỚI, theo pattern conventions.zh.mdx có sẵn)
- Modify: `README.md` (+1 dòng link sang bản tiếng Việt — additive)

- [ ] **Step 1: Copy 6 docs .vi.md từ app-hira về repo root**, đọc lướt từng file sửa: URL `app.hira.vn` (giữ nếu đúng domain), các lệnh/flag CLI đã đổi giữa 2 thời điểm fork (đối chiếu `multica --help` hiện tại).
- [ ] **Step 2: Tạo `conventions.vi.mdx`** — dịch phần glossary của `conventions.mdx` sang VI + chèn bảng Glossary EN↔VI ở đầu plan này làm chuẩn dịch thuật cho agent/người dịch sau.
- [ ] **Step 3:** `pnpm --filter @multica/docs build` (nếu docs app có script build) — xanh.
- [ ] **Step 4: Commit** — `git commit -m "docs: add Vietnamese documentation and translation glossary"`

---

## Task 16: Hoàn thiện BRANDING.md + quy trình sync upstream

**Files:**
- Modify: `BRANDING.md` (bảng registry đầy đủ + playbook)

- [ ] **Step 1: Soát bảng registry đủ mọi touch-point** (đối chiếu `git diff main --stat` — mọi file sửa mà không phải file mới đều phải có mặt):

Bảng cuối cùng phải gồm đúng các file này:

```markdown
## Touch-point Registry
| File | Thay đổi | Chính sách khi conflict |
|---|---|---|
| packages/core/i18n/types.ts | +"vi" (2 dòng) | Re-apply |
| packages/core/i18n/pick-locale.test.ts | +1 it() vi | Giữ cả hai phía |
| server/internal/handler/auth.go | +"vi" supportedLanguages | Re-apply 1 dòng |
| server/internal/handler/user_language_test.go | +1 test vi | Giữ cả hai phía |
| packages/views/locales/index.ts | +25 import + block vi | Re-apply block |
| packages/views/locales/{en,zh-Hans,ko,ja}/settings.json | +key "vietnamese" | Re-apply 1 key/file |
| packages/views/settings/components/preferences-tab.tsx | +1 option vi | Re-apply 1 dòng |
| apps/web/app/layout.tsx | font subsets + metadata Hira + HTML_LANG vi | Lấy upstream → re-apply 3 vùng |
| apps/desktop/src/renderer/src/App.tsx | +HTML_LANG vi | Re-apply 1 dòng |
| packages/views/onboarding/templates/index.ts | +vi→en fallback | Re-apply 1 dòng |
| apps/web/lib/use-cases-i18n.ts | +entry vi | Re-apply block |
| apps/web/app/globals.css | +@import brand.css | Re-apply 1 dòng |
| apps/desktop/src/renderer/src/globals.css | +@import brand.css | Re-apply 1 dòng |
| server/internal/service/email.go | sender + 2 subject Hira/VI | Lấy upstream → re-apply 3 chuỗi |
| packages/ui/components/common/multica-icon.tsx | SVG Hira, GIỮ export name | Re-apply SVG |
| apps/web/public/favicon.svg | asset Hira | merge=ours (tự động) |
| apps/desktop/{package.json,electron-builder.yml,src/main/index.ts} | productName/appId/publish | Lấy upstream → re-apply theo Task 14 |
| README.md | +1 dòng link bản VI | Re-apply 1 dòng |
```

- [ ] **Step 2: Viết playbook sync vào BRANDING.md**

```markdown
## Quy trình sync upstream (mỗi lần cần tính năng mới)

    git fetch upstream
    git checkout main
    git merge upstream/main          # KHÔNG rebase — giữ lịch sử fork
    # Conflict? → mở bảng Touch-point Registry, xử từng file theo cột "Chính sách"
    pnpm install                     # lockfile/catalog có thể đổi
    pnpm typecheck                   # BẮT BUỘC: map Record<SupportedLocale> mới thiếu vi → compiler chỉ chỗ
    pnpm test                        # BẮT BUỘC: parity test liệt kê key EN mới chưa có trong vi
    #   → upstream thêm namespace mới? copy en/<ns>.json sang vi/, đăng ký index.ts, dịch
    #   → upstream thêm key mới? key đã tồn tại trong vi (nếu copy từ en khi merge báo) — dịch value
    cd server && go test ./...
    make check                       # full pipeline trước khi push

## Ba lưới an toàn tự động sau merge
1. `pnpm typecheck` — bắt mọi map locale mới thiếu `vi` (compile error).
2. `parity.test.ts` — bắt mọi key/namespace EN mới chưa nhập vào `vi` (test fail kèm danh sách key).
3. `git config merge.ours.driver true` + `.gitattributes` — asset brand không bao giờ bị upstream ghi đè.

## Điều CẤM (bài học từ app-hira)
- Không đổi tên @multica/*, CLI, Go module, env, DB, cookie.
- Không sửa tokens.css / base.css — mọi override vào brand.css.
- Không sửa chuỗi trong locales/en|zh-Hans|ko|ja (trừ key "vietnamese" đã đăng ký).
- Không viết lại page/component upstream chỉ để đổi style — override bằng token/CSS trước.
- Mỗi lần buộc sửa file upstream: thêm dòng vào Touch-point Registry NGAY trong commit đó.
```

- [ ] **Step 3: Diễn tập sync** — kiểm chứng yêu cầu "update fork sync khi cần tính năng mới":

```bash
git fetch upstream
git merge upstream/main --no-edit   # trên nhánh hira/viet-hoa
pnpm typecheck && pnpm test && (cd server && go test ./...)
```

Expected: merge sạch hoặc chỉ conflict trong các file thuộc registry; sau khi xử theo playbook, toàn bộ checks xanh.

- [ ] **Step 4: Commit cuối + full check**

```bash
make check
git add BRANDING.md && git commit -m "docs(fork): complete touch-point registry and upstream sync playbook"
```

---

## (Tùy chọn — làm sau khi mọi thứ trên xong) Task 17: Font display Space Grotesk + agent avatar Hira

- Space Grotesk cho heading: thêm vào `apps/web/app/layout.tsx` `const spaceGrotesk = Space_Grotesk({ subsets: ["latin", "vietnamese"], variable: "--font-display", weight: ["500","600","700"] })`, gắn `spaceGrotesk.variable` vào className của `<html>`, rồi trong `brand.css` thêm `:root { --font-heading: var(--font-display), var(--font-sans); }` — ghi registry.
- Agent avatar gradient (amber/sky/mint/rose/violet theo role): copy `packages/ui/styles/hira-scope.css` từ app-hira thành file fork-owned mới, import trong brand.css (`@import "./hira-scope.css";`). Chỉ làm nếu chấp nhận khác biệt giao diện với upstream (style thuần CSS class mới — không sửa component upstream thì conflict = 0).

---

## Ước lượng & thứ tự thực thi

| Giai đoạn | Task | Khối lượng |
|---|---|---|
| Hạ tầng i18n + wiring | 1–6 | ~½ ngày |
| Dịch 3.300 chuỗi (AI dịch + người duyệt theo glossary) | 7–10 | 1–2 ngày |
| Brand overlay + metadata + email | 11–13 | ~½ ngày |
| Desktop (nếu ship) | 14 | ~½ ngày |
| Docs + playbook + diễn tập sync | 15–16 | ~½ ngày |

Tổng: **2,5–4 ngày**, deliver được theo từng PR nhỏ (mỗi task hoặc cụm task là 1 PR vào `main` của fork).
