import type { CreateSkillRequest } from "@multica/core/types";

export interface SkillTemplate {
  id: string;
  name: string;
  description: string;
  icon: "globe" | "file-search";
  data: CreateSkillRequest;
}

const AGENT_BROWSER_CONTENT = `# Browser Automation with agent-browser

Use this skill for visual validation of UI changes — launch the app, walk through user flows, capture screenshots, and upload evidence to the issue.

## Setup

The \`agent-browser\` CLI is pre-installed in your environment when this skill is assigned.

## Core Workflow

Every browser automation follows this pattern:

1. **Navigate**: \`agent-browser open <url>\`
2. **Snapshot**: \`agent-browser snapshot -i\` (get element refs like \`@e1\`, \`@e2\`)
3. **Interact**: Use refs to click, fill, select
4. **Re-snapshot**: After navigation or DOM changes, get fresh refs

\`\`\`bash
agent-browser open http://localhost:3000/login
agent-browser snapshot -i
# Output: @e1 [input type="email"], @e2 [input type="password"], @e3 [button] "Sign In"

agent-browser fill @e1 "user@example.com"
agent-browser fill @e2 "password123"
agent-browser click @e3
agent-browser wait --load networkidle
agent-browser snapshot -i  # Check result
\`\`\`

## Essential Commands

\`\`\`bash
# Navigation
agent-browser open <url>              # Navigate to URL
agent-browser close                   # Close browser

# Snapshot (always do this after navigation)
agent-browser snapshot -i             # Interactive elements with refs
agent-browser snapshot -i -C          # Include cursor-interactive elements

# Interaction (use @refs from snapshot)
agent-browser click @e1               # Click element
agent-browser fill @e2 "text"         # Clear and type text
agent-browser select @e1 "option"     # Select dropdown option
agent-browser check @e1               # Check checkbox
agent-browser press Enter             # Press key
agent-browser scroll down 500         # Scroll page

# Wait
agent-browser wait @e1                # Wait for element
agent-browser wait --load networkidle # Wait for network idle
agent-browser wait --url "**/page"    # Wait for URL pattern

# Capture
agent-browser screenshot              # Screenshot to working directory
agent-browser screenshot --full       # Full page screenshot
\`\`\`

## Visual Validation for Multica Issues

When your task involves UI changes, follow this flow:

### 1. Launch the dev server

\`\`\`bash
pnpm dev:web &
DEV_PID=$!
sleep 10
agent-browser open http://localhost:3000
agent-browser wait --load networkidle
\`\`\`

### 2. Walk through the changed UI flow

Navigate to the pages affected by your changes. At each key step:

\`\`\`bash
agent-browser open http://localhost:3000/path/to/changed/page
agent-browser wait --load networkidle
agent-browser snapshot -i
agent-browser click @e3
agent-browser wait --load networkidle
agent-browser screenshot --full step1.png
\`\`\`

### 3. Upload evidence to the issue

\`\`\`bash
multica attachment upload step1.png --issue <issue-id>
multica issue comment add <issue-id> --content "Visual validation complete." --attachment screenshot.png
\`\`\`

### 4. Clean up

\`\`\`bash
agent-browser close
kill $DEV_PID 2>/dev/null
\`\`\`

## Ref Lifecycle

Refs (\`@e1\`, \`@e2\`, etc.) are invalidated when the page changes. Always re-snapshot after clicking links, form submissions, or dynamic content loading.

## When to Use This Skill

- **Always** when your changes touch \`.tsx\`, \`.css\`, or any UI-related files
- **Always** when the issue description mentions visual changes, layout, or design
- **Skip** for pure backend changes, config changes, or non-visual refactors
`;

const UI_REVIEW_CONTENT = `# UI Review

Run this skill after every UI change, before committing or opening a PR. It consolidates the full quality pipeline into a single pass.

## Scope

Identify which files changed:

\`\`\`bash
git --no-pager diff --name-only HEAD | grep -E '\\.(tsx|css)$'
\`\`\`

Read every changed \`.tsx\` file completely before starting.

## Pipeline

Run each check below **in order**. For each check, report findings as a markdown table: \`| Finding | Severity | Line | Recommendation |\`. Severity levels: 🔴 Critical, 🟠 High, 🟡 Medium, 🟢 Low.

After all checks, produce a **summary scorecard** and a list of **fixes to apply**.

Then **implement all fixes** that are Critical or High severity.

After fixing, run \`pnpm typecheck && pnpm test\` to verify nothing broke.

---

### 1. Accessibility audit

For every interactive element in the changed components:

- Does it have an accessible name? (\`aria-label\`, visible text, \`htmlFor\`/\`id\`)
- Does it meet 44×44px minimum touch target?
- Does it have focus-visible styles?
- Do all images have \`alt\` text?
- Do decorative SVGs have \`aria-hidden="true"\`?

### 2. Responsiveness

Trace the layout at three viewport widths: 320px (phone), 768px (tablet), 1024px+ (desktop).

### 3. Dark mode and theming

- No hard-coded color classes — use design tokens (\`text-foreground\`, \`bg-background\`, etc.)
- CSS variable tokens auto-switch and don't need \`dark:\` variants

### 4. Design system compliance

- Dialog structure, loading buttons, button sizes, icon sizes, toast format
- Package boundaries: \`packages/ui/\` no \`@multica/core\`, \`packages/views/\` no \`next/*\`

### 5. Performance

- \`useCallback\`/\`useMemo\` for handlers passed as props
- No inline arrow functions defeating \`React.memo\`

### 6. Copy clarity

- Sentence case, actionable error messages, platform-aware keyboard shortcuts

### 7. Error handling

- Loading, error, and success states for async operations
- Double-submit guards on form buttons

### 8. Animation and motion

- GPU-accelerated properties only, durations under 300ms for feedback
- \`motion-reduce:\` support

---

## Output format

\`\`\`markdown
## UI Review Summary

| Check | Status | Issues |
|-------|--------|--------|
| Accessibility | ✅/⚠️/❌ | N issues |
| Responsiveness | ✅/⚠️/❌ | N issues |
| Dark mode | ✅/⚠️/❌ | N issues |
| Design system | ✅/⚠️/❌ | N issues |
| Performance | ✅/⚠️/❌ | N issues |
| Copy clarity | ✅/⚠️/❌ | N issues |
| Error handling | ✅/⚠️/❌ | N issues |
| Animation | ✅/⚠️/❌ | N issues |

**Score: N/10**
\`\`\`

Then implement Critical and High fixes, run \`pnpm typecheck && pnpm test\`, and report results.
`;

export const SKILL_TEMPLATES: SkillTemplate[] = [
  {
    id: "agent-browser",
    name: "Agent Browser",
    description: "Browser automation for visual validation of UI changes",
    icon: "globe",
    data: {
      name: "Agent Browser",
      description: "Browser automation for visual validation of UI changes — launch the app, walk through user flows, capture screenshots, and upload evidence.",
      content: AGENT_BROWSER_CONTENT,
      config: { requires_browser: true },
    },
  },
  {
    id: "ui-review",
    name: "UI Review",
    description: "Comprehensive UI quality review before commit or PR",
    icon: "file-search",
    data: {
      name: "UI Review",
      description: "Run a comprehensive UI quality review on changed components — accessibility, responsiveness, dark mode, design system compliance, and more.",
      content: UI_REVIEW_CONTENT,
    },
  },
];
