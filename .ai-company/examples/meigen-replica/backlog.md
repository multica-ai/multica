# Backlog — meigen-replica (chenzh/meigen-replica)

> Visual replica of meigen.ai gallery + studio. DoD = `accept_cases.md` + `make visual-check`.  
> TICKET-001 已 merge；002–003 在 GitHub 进行中。续票供 Work-Finder / nightly sync。

### TICKET-001 [agent-safe] Harness + agent-delivery CI

- **What:** Install company harness (workflows, agent-delivery scripts, labels)
- **DoD:** `make check` + `make visual-check` green in CI
- **Status:** merged (PR #4)

### TICKET-002 [agent-safe] Visual gate baseline sign-off (desktop + mobile)

- **What:** Confirm `@visual` baselines; check AC-V1 / AC-V2 in accept_cases.md
- **DoD:** `make visual-check` exit 0; AC-V1/V2 checked

### TICKET-003 [agent-safe] Visual break-guard regression

- **What:** Ensure intentional H1 break fails visual-check (AC-V3 documented)
- **DoD:** `make visual-check` fails on break, passes after restore

### TICKET-004 [agent-safe] Gallery card detail overlay polish

- **Owner:** Implementer
- **What:** 卡片点击打开 detail overlay（标题、模型标签、关闭）；375 宽可用
- **AC / DoD:** AC 可手测；`make visual-check` exit 0
- **Source:** backlog seed 2026-08-29

### TICKET-005 [agent-safe] Locale persistence (zh/en)

- **Owner:** Implementer
- **What:** `localStorage` 记住语言选择；刷新后保持
- **AC / DoD:** `make check` exit 0；切换流程手测通过
- **Source:** backlog seed 2026-08-29

### TICKET-006 [agent-safe] Skills wizard keyboard / a11y

- **Owner:** Implementer
- **What:** Skills 步骤控件补 `aria-label`；Esc 关闭 overlay
- **AC / DoD:** `make check` exit 0
- **Source:** backlog seed 2026-08-29

### TICKET-007 [agent-safe] Studio mobile 375 layout

- **Owner:** Implementer
- **What:** `/#studio` 在 375 宽下单列可用（dock 不溢出）
- **AC / DoD:** `make visual-check` exit 0（补 mobile studio 快照若需要）
- **Source:** backlog seed 2026-08-29

### TICKET-008 [agent-safe] OG / Twitter meta (gallery home)

- **Owner:** Implementer
- **What:** `index.html` 补 title、description、openGraph/twitter 占位
- **AC / DoD:** `make check` exit 0
- **Source:** backlog seed 2026-08-29
