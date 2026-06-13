# Báo cáo Việt hóa UI — Multica

**Ứng dụng:** Multica — Project Management for Human + Agent Teams
**URL test:** `http://localhost:3000`
**Ngày test:** 21/04/2026
**Phương pháp:** Crawl bằng Chrome extension MCP + đọc dump wget (`/Users/dev/Claude/localhost3000`) + phân tích 15 screenshot đính kèm
**Phạm vi:** 11 route trong app + landing page + login

---

## 1. Tổng quan nhanh

| Cụm màn hình | Mức độ Việt hóa | Ưu tiên sửa |
|---|---|---|
| Landing page (`/`) + marketing (`/about`, `/changelog`) | **0%** | Thấp (public, có thể giữ EN) |
| Login (`/login`) | **0%** | Trung bình |
| App shell (sidebar, topbar, user menu, AI panel) | **~5%** | **Cao nhất** |
| Issues list & Kanban (`/space/issues`, `/space/my-issues`) | **0%** | **Cao** |
| Projects (`/space/projects`) | **0%** | Cao |
| Inbox (`/space/inbox`) | **~40%** hỗn hợp | **Cao** (do inconsistent) |
| Autopilot (`/space/autopilots`) | **~85%** | Thấp |
| Agents (`/space/agents`) | **~35%** hỗn hợp | Cao |
| Runtimes / Skills | Chưa test được (bị logout giữa chừng) | — |
| Settings (`/space/settings`) | **0%** | Cao |

**Hằng số chính:** Hầu hết **status, priority, action chung** (Backlog, Todo, In Progress, In Review, Done, Blocked, Cancelled, Urgent, High, Medium, Low, No priority, Save, Continue, Log out, …) chưa được dịch ở bất kỳ đâu. Đây là các chuỗi xuất hiện ở nhiều chỗ nên sửa 1 lần sẽ tác động lớn nhất.

**Phát hiện nổi bật:**
- `<html lang="vi">` nhưng toàn bộ content landing/login/shell là tiếng Anh → thẻ lang sai lệch, ảnh hưởng SEO + screen reader + browser translate prompt.
- Language switcher ở footer landing chỉ có **EN / 中文**, **thiếu lựa chọn VI** dù sản phẩm hướng đến người Việt.
- Có hiện tượng "dịch nửa câu" (VD Inbox: *"Đặt trạng thái thành **In Review**"*) → cần policy rõ về việc có dịch enum/status hay không.
- Autopilot đã Việt hóa rất tốt — nên lấy làm **reference style** cho các trang còn lại.

---

## 2. Vấn đề toàn cục (xuất hiện trên MỌI trang trong app)

### 2.1. Sidebar điều hướng — 100% tiếng Anh
Nhóm Workspace: `Inbox`, `My Issues`, `Issues`, `Projects`, `Autopilot`, `Agents`
Nhóm Configure: `Runtimes`, `Skills`, `Settings`

### 2.2. Topbar — 100% tiếng Anh
- `Search...` (placeholder)
- `New Issue` (nút + shortcut `C`)
- `Toggle Sidebar` (aria-label + tooltip)

### 2.3. User menu / workspace dropdown (Image 15)
`Workspaces`, `Create workspace`, `Log out`

### 2.4. Filter / View dropdowns (Image 8–13) — 100% tiếng Anh
- Menu gốc: `Status`, `Priority`
- Status values: `Backlog`, `Todo`, `In Progress`, `In Review`, `Done`, `Blocked`, `Cancelled`
- Priority values: `Urgent`, `High`, `Medium`, `Low`, `No priority`
- View switcher: `View`, `Board`, `List`
- Sort panel: `Ordering`, `Manual`
- Card properties toggle: `Card properties`, `Priority`, `Description`, `Assignee`, `Due date`, `Project`, `Sub-issue progress`

### 2.5. Panel Deep Research Agent (bên phải, luôn hiển thị)
- Heading: `Hi, I'm Deep Research Agent`
- Subheading: `Try asking`
- Nút: `New chat`
- 3 gợi ý prompt: `📋 List my open tasks by priority`, `📝 Summarize what I did today`, `💡 Plan what to work on next`
- Status chip: `Idle`

### 2.6. HTML metadata
- `<title>Multica — Project Management for Human + Agent Teams</title>`
- `<meta name="description">Open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.</meta>`
- `<meta property="og:locale" content="en_US"/>` (nên là `vi_VN` khi locale là VI)

### 2.7. Accessibility strings (ARIA)
Chuỗi hướng dẫn drag-and-drop bằng bàn phím cho kanban — màn hình sẽ đọc khi dùng screen reader:
> "To pick up a draggable item, press the space bar. While dragging, use the arrow keys to move the item. Press space again to drop the item in its new position, or press escape to cancel."

---

## 3. Vấn đề theo từng trang

### 3.1. Landing `/` (index.html)

**Trạng thái:** 100% tiếng Anh, dù `lang="vi"`. Phát hiện thêm một language switcher ở footer có **EN + 中文** nhưng không có VI.

**Các chuỗi chính cần dịch:**
- Hero H1: `Your next 10 hires won't be human.`
- Hero sub: `Multica is an open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills — manage your human + agent workforce in one place.`
- CTA: `Start free trial`, `Download Desktop`, `Log in`, `Get started`, `Get started`, `View on GitHub`, `Star on GitHub`
- 4 cột tính năng (H2):
  1. `Assign to an agent like you'd assign to a colleague`
  2. `Set it and forget it — agents work while you sleep`
  3. `Every solution becomes a reusable skill for the whole team`
  4. `One dashboard for all your compute`
- Cột điều hướng trái: `TEAMMATES`, `AUTONOMOUS`, `SKILLS`, `RUNTIMES`
- Section `Get started` với 4 bước `01`–`04`: `Sign up & create your workspace`, `Install the CLI & connect your machine`, `Create your first agent`, `Assign an issue and watch it work`
- Section `Open source`: tất cả 4 card
- FAQ section: 6 câu hỏi + 6 câu trả lời (100% EN)
- Footer: `Product`, `Resources`, `Company`, các link + `© 2026 Multica. All rights reserved.`

**Copy marketing trong demo preview** (có thể giữ EN vì là demo giả lập môi trường dev, nhưng nên có bản song ngữ):
- "Refactor API error handling middleware", "assigned to Claude", "changed status from Todo to In Progress", "Agent is working", "Task execution history", …

### 3.2. Login `/login` (dump là bailout; text lấy từ Chrome)

- `Sign in to Multica`
- `Enter your email to get a login code`
- Field: `Email`
- Nút: `Continue`

### 3.3. `/space/issues` (Home app, redirect default)

**100% tiếng Anh:**
- Breadcrumb: `space › Issues`
- Tab filter: `All`, `Members`, `Agents`
- Tooltip khi hover tab Members: `Issues assigned to team members` (Image 7)
- Kanban columns: `Backlog 0`, `Todo 0`, `In Progress 0`, `In Review 2`, `Done 0`
- Empty state mỗi cột: `No issues`
- Empty state list view: `No issues yet`, `Create an issue to get started.`
- Badges trên card: `No priority`, `— No priority`
- Issue IDs: `SPA-1`, `SPA-2` (prefix là tên workspace — OK không cần dịch)

### 3.4. `/space/my-issues`

**100% tiếng Anh** (ngoài sidebar):
- Breadcrumb: `space › My Issues`
- Tab filter: `Assigned`, `Created`, `My Agents`
- Tất cả status column giống Issues
- ARIA drag-drop hint (mục 2.7)

### 3.5. `/space/projects`

**100% tiếng Anh:**
- H1: `Projects`
- Nút: `New project`
- Empty state: `No projects yet`, `Create your first project`

**Modal New Project (Image 6):**
- Breadcrumb: `space › New project`
- Placeholder: `Project title`, `Add description...`
- Badges: `Planned` (status), `No priority`, `Lead`
- Nút: `Create Project`

### 3.6. `/space/inbox` — ⚠️ **HỖN HỢP, ƯU TIÊN SỬA**

**Đã Việt hóa:**
- H1: `Hộp thư đến` ✅
- Empty/hint: `Chọn thông báo để xem chi tiết` ✅
- Action chip trong card: `Đặt trạng thái thành [...]` ✅

**Chưa dịch (gây inconsistency):**
- Giá trị trạng thái nhúng trong câu tiếng Việt: `In Review` → câu trở thành *"Đặt trạng thái thành **In Review**"*
- Thời gian tương đối: `1h`, `2h` (nên là `1 giờ`, `2 giờ` hoặc giữ ngắn — cần quyết định)

### 3.7. `/space/autopilots` — ✅ **GẦN HOÀN THIỆN**

**Đã dịch tốt (lấy làm reference):**
- H1 `Autopilot`, nút `Autopilot mới`, `Tạo từ đầu`, `Hủy`, `Tạo`
- Empty state: `Chưa có autopilot nào`, `Lên lịch các tác vụ định kỳ cho AI agents. Chọn template hoặc tạo từ đầu.`
- 6 template card (tên + mô tả 100% Việt): Tổng hợp tin tức / Nhắc review pull request / Phân loại bug / Báo cáo tiến độ hằng tuần / Kiểm tra dependency / Kiểm tra tài liệu
- Form fields: `Tên`, `Lịch chạy` ✅

**Chưa dịch (modal New Autopilot — Image 5):**
- Label: `Prompt`, `Agent`, `Time`, `Timezone`
- Placeholder: `Chọn agent...` ✅ nhưng `Runs daily at 9:00 AM GMT+7` (helper text dưới) chưa dịch
- Schedule options: `Hourly`, `Daily` ✅ (bold), `Weekdays`, `Days`, `Custom`

### 3.8. `/space/agents` — ⚠️ **HỖN HỢP**

**Đã dịch:**
- Tab: `Hướng dẫn`, `Môi trường`, `Tùy biến Args`, `Cài đặt` ✅
- (Ghi chú: tên tab `Tùy biến Args` mix Việt-Anh không tự nhiên, nên cân nhắc `Tham số` hoặc `Args tuỳ chỉnh`)

**Chưa dịch:**
- Tab còn lại: `Skills`, `Tasks`
- Status chip: `Idle`
- H2/H3: `Agents`, `Agent Instructions`
- Mô tả Agent Instructions: `Define this agent's identity and working style. These instructions are injected into the agent's context for every task.`
- Empty state: `No instructions set`
- Placeholder textarea (dài, toàn bộ là EN): `Define this agent's role, expertise, and working style. Example: You are a frontend engineer specializing in React and TypeScript. ## Working Style ...`
- Nút: `Save`
- Tab Tùy biến Args (Image 2): heading `Custom Arguments`, mô tả `Additional CLI arguments appended to the agent command at launch. Supported flags depend on the agent's CLI.`, label `Launch mode:`, nút `+ Add`, `Save`

### 3.9. `/space/settings` (Image 1) — **100% TIẾNG ANH**

**General section:**
- Heading: `General`
- Field labels: `Name`, `Description`, `Context`, `Slug`
- Placeholders: `What does this workspace focus on?`, `Background information and context for AI agents working in this workspace`
- Nút: `Save`

**Danger Zone section:**
- Heading: `Danger Zone`
- Item 1: `Leave workspace` + `You're the only owner. Promote another member to owner first, or delete the workspace.` + nút `Leave workspace`
- Item 2: `Delete workspace` + `Permanently delete this workspace and its data.` + nút `Delete workspace`

### 3.10. `/space/runtimes`, `/space/skills`

Bị logout giữa phiên nên không crawl được trực tiếp. Từ landing demo preview và sidebar, dự đoán tương tự pattern: empty state + filter + action buttons đều là EN. **Cần test riêng** sau khi hoàn thiện các trang khác.

### 3.11. `/space/issues/[id]` (trang chi tiết issue)

Không crawl được trực tiếp lần này, nhưng từ landing demo preview có thể thấy các chuỗi:
- `Properties`, `Status`, `Priority`, `Assignee`
- `Assign to...`, `Unassigned`, `Members`, `Agents`
- `Activity`, `Subscribe`

→ Cần test chi tiết sau.

---

## 4. Bảng tổng hợp chuỗi cần dịch (glossary đề xuất)

Để đảm bảo consistency, đề xuất chuẩn hóa glossary sau và áp dụng toàn bộ app:

### 4.1. Navigation & shell

| EN | VN đề xuất | Ghi chú |
|---|---|---|
| Inbox | Hộp thư đến | Đã dùng trong Inbox page |
| My Issues | Việc của tôi | Hoặc "Issue của tôi" nếu giữ jargon |
| Issues | Công việc / Issues | Cân nhắc giữ nguyên "Issues" vì là jargon dev |
| Projects | Dự án | |
| Autopilot | Autopilot | Giữ (đã dùng) |
| Agents | Agents | Giữ (jargon AI) |
| Runtimes | Runtimes | Giữ hoặc "Máy chạy" |
| Skills | Skills | Giữ (tên feature) |
| Settings | Cài đặt | Đã dùng trong tab Agent |
| Search... | Tìm kiếm... | |
| New Issue | Tạo Issue | |
| Toggle Sidebar | Bật/tắt thanh bên | |
| Log out | Đăng xuất | |
| Create workspace | Tạo workspace | |
| Workspaces | Các workspace | |

### 4.2. Status (dùng ở Kanban, Inbox, Issue detail, filter)

| EN | VN đề xuất |
|---|---|
| Backlog | Tồn đọng |
| Todo | Cần làm |
| In Progress | Đang làm |
| In Review | Đang duyệt |
| Done | Hoàn tất |
| Blocked | Bị chặn |
| Cancelled | Đã hủy |
| Planned | Đã lên kế hoạch |
| Idle | Rảnh / Nhàn rỗi |

### 4.3. Priority

| EN | VN đề xuất |
|---|---|
| Urgent | Khẩn cấp |
| High | Cao |
| Medium | Trung bình |
| Low | Thấp |
| No priority | Không ưu tiên |

### 4.4. Actions & properties

| EN | VN đề xuất |
|---|---|
| Save | Lưu |
| Continue | Tiếp tục |
| Create Project | Tạo dự án |
| New project | Dự án mới |
| Cancel / Hủy | Hủy (đã dùng trong Autopilot) |
| Add | Thêm |
| Delete | Xóa |
| Edit | Sửa |
| Lead | Người phụ trách |
| Assignee | Người được giao |
| Priority | Độ ưu tiên |
| Status | Trạng thái |
| Description | Mô tả |
| Due date | Hạn |
| Project | Dự án |
| Sub-issue progress | Tiến độ issue con |
| Card properties | Thuộc tính thẻ |
| Ordering | Sắp xếp |
| Manual | Thủ công |
| View / Board / List | Giao diện / Bảng / Danh sách |

### 4.5. Empty states & micro-copy

| EN | VN đề xuất |
|---|---|
| No issues | Không có issue |
| No issues yet | Chưa có issue nào |
| Create an issue to get started. | Tạo issue để bắt đầu. |
| No projects yet | Chưa có dự án nào |
| Create your first project | Tạo dự án đầu tiên của bạn |
| No instructions set | Chưa đặt hướng dẫn |

---

## 5. Khuyến nghị & lộ trình

### Ưu tiên 1 — App shell (tác động lớn nhất, sửa 1 lần)
- Sidebar 9 mục
- Topbar (Search, New Issue, Toggle Sidebar)
- User/workspace dropdown (Image 15)
- Panel Deep Research Agent (xuất hiện mọi trang)
- Page title HTML + `og:locale`

### Ưu tiên 2 — Enum chung (Status/Priority)
Centralize tất cả status và priority strings trong 1 file i18n, dùng chung cho kanban, filter dropdown, inbox, issue card, issue detail. Sửa ở đây sẽ **đồng loạt** vá được Issues, My Issues, Inbox, detail modal, landing demo.

### Ưu tiên 3 — Các trang empty-state quan trọng
- `/space/issues`, `/space/my-issues`, `/space/projects`: empty state + CTA
- Modal "New project" (Image 6)
- Settings page (Image 1) toàn bộ

### Ưu tiên 4 — Dọn inconsistency
- Inbox: hoàn thiện phần "In Review" trong câu Việt
- Agents: dịch các tab còn sót (`Skills`, `Tasks`), `Save`, placeholder textarea dài, `Agent Instructions`, `No instructions set`, `Custom Arguments`, `Launch mode`
- Autopilot modal: `Prompt`, `Weekdays/Days/Custom`, `Time`, `Timezone`, helper text `Runs daily at…`

### Ưu tiên 5 — Quyết định chính sách (trước khi dịch tiếp)
Cần chốt 2 câu hỏi để nhất quán toàn app:

1. **Có giữ jargon dev nguyên bản không?** Ví dụ: `pull request`, `bug`, `dependency`, `runtime`, `skill`, `PR`, `issue`, `workspace`, `agent`. Khuyến nghị: giữ nguyên các thuật ngữ kỹ thuật nhưng dịch câu/mô tả xung quanh (giống cách Autopilot đang làm).
2. **Có dịch status enum không?** Nếu có, dùng bảng 4.2 ở trên. Nếu không, ít nhất phải thêm i18n layer để người dùng EN/VI thấy nhất quán.

### Ưu tiên 6 — Landing & marketing
Landing là cửa ngõ SEO + pitch → cân nhắc làm bản song ngữ. Nếu sản phẩm target thị trường Việt, **thêm VI vào language switcher** (hiện chỉ có EN/中文). Đổi `og:locale` → `vi_VN` khi người dùng ở VI.

### Kỹ thuật — đề xuất triển khai
- Dùng `next-intl` hoặc `react-i18next` thay vì hardcode string (quan sát thấy đã có `LocaleSync`, `LocaleProvider`, `initialLocale: "en"` trong bundle React — hạ tầng i18n đã có sẵn, chỉ cần bổ sung file `vi.json`).
- Tập trung tất cả chuỗi Status/Priority/Actions vào namespace chung (`common.json`) để reuse.
- Sửa `<html lang>` theo locale thực (hiện là `vi` hardcode nhưng UI lại render EN → gây xung đột với browser translate và screen reader).
- Bổ sung lựa chọn VI vào language switcher ở footer landing.

---

## 6. Hạn chế của báo cáo này

- **Runtimes, Skills, Issue detail** chưa test được do bị logout giữa phiên (cần OTP email để đăng nhập lại).
- **Modal/popover sâu** (ví dụ: assignee picker, status change picker, context menu trên issue card) chỉ quan sát được một số qua screenshot; có thể còn chuỗi chưa liệt kê.
- **Trang error** (404, 500), **toast notifications**, **form validation messages** chưa được kiểm tra riêng.
- **Landing demo preview** có rất nhiều chuỗi "fake data" tiếng Anh (`Refactor API error handling middleware`, activity feed) — có thể là cố ý giữ EN vì là marketing mockup, nhưng cần chốt chính sách.

Nếu cần tiếp tục crawl 3 trang còn thiếu + các modal, bạn cho tôi biết để chạy tiếp trong phiên mới.
