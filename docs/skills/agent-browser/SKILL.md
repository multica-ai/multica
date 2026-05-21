---
name: agent-browser
description: Browser automation CLI for AI agents. Use when the user needs to interact with websites, including navigating pages, filling forms, clicking buttons, taking screenshots, extracting data, testing web apps, or automating any browser task. Triggers include requests to "open a website", "fill out a form", "click a button", "take a screenshot", "scrape data from a page", "test this web app", "login to a site", "automate browser actions", or any task requiring programmatic web interaction.
allowed-tools: Bash(agent-browser:*)
---

# Browser Automation with agent-browser

Use this skill for visual validation of UI changes — launch the app, walk through user flows, capture screenshots, and upload evidence to the issue.

## Setup

The `agent-browser` CLI is pre-installed in your environment when this skill is assigned.

## Core Workflow

Every browser automation follows this pattern:

1. **Navigate**: `agent-browser open <url>`
2. **Snapshot**: `agent-browser snapshot -i` (get element refs like `@e1`, `@e2`)
3. **Interact**: Use refs to click, fill, select
4. **Re-snapshot**: After navigation or DOM changes, get fresh refs

```bash
agent-browser open http://localhost:3000/login
agent-browser snapshot -i
# Output: @e1 [input type="email"], @e2 [input type="password"], @e3 [button] "Sign In"

agent-browser fill @e1 "user@example.com"
agent-browser fill @e2 "password123"
agent-browser click @e3
agent-browser wait --load networkidle
agent-browser snapshot -i  # Check result
```

## Essential Commands

```bash
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
```

## Visual Validation for Multica Issues

When your task involves UI changes, follow this flow:

### 1. Launch the dev server

```bash
# Start the frontend dev server in background
pnpm dev:web &
DEV_PID=$!

# Wait for it to be ready
sleep 10
agent-browser open http://localhost:3000
agent-browser wait --load networkidle
```

### 2. Walk through the changed UI flow

Navigate to the pages affected by your changes. At each key step:

```bash
# Navigate to the affected page
agent-browser open http://localhost:3000/path/to/changed/page
agent-browser wait --load networkidle
agent-browser snapshot -i

# Interact with changed elements
agent-browser click @e3
agent-browser wait --load networkidle

# Capture screenshot as evidence
agent-browser screenshot --full step1-before.png
```

### 3. Capture evidence at key points

Take screenshots at:
- **Before state** (if applicable): The page before your changes
- **After state**: The page with your changes applied
- **Interaction result**: After clicking buttons, submitting forms, etc.
- **Edge cases**: Empty states, error states, loading states

```bash
# Full page screenshot
agent-browser screenshot --full overview.png

# Screenshot of specific area (use snapshot to identify refs)
agent-browser screenshot result.png
```

### 4. Upload evidence to the issue

```bash
# Upload screenshots as issue attachments
multica attachment upload step1-before.png --issue <issue-id>
multica attachment upload step2-after.png --issue <issue-id>

# Include evidence in your issue comment
multica issue comment add <issue-id> --content "Visual validation complete. Screenshots attached showing the fix." --attachment validation-screenshot.png
```

### 5. Clean up

```bash
agent-browser close
kill $DEV_PID 2>/dev/null
```

## Ref Lifecycle (Important)

Refs (`@e1`, `@e2`, etc.) are invalidated when the page changes. Always re-snapshot after:

- Clicking links or buttons that navigate
- Form submissions
- Dynamic content loading (dropdowns, modals)

```bash
agent-browser click @e5              # Navigates to new page
agent-browser snapshot -i            # MUST re-snapshot
agent-browser click @e1              # Use new refs
```

## Common Patterns

### Form Submission Validation

```bash
agent-browser open http://localhost:3000/issues/new
agent-browser snapshot -i
agent-browser fill @e1 "Test Issue Title"
agent-browser fill @e2 "Issue description"
agent-browser screenshot --full before-submit.png
agent-browser click @e3  # Submit button
agent-browser wait --load networkidle
agent-browser screenshot --full after-submit.png
```

### Authentication Flow

```bash
agent-browser open http://localhost:3000/login
agent-browser snapshot -i
agent-browser fill @e1 "test@example.com"
agent-browser fill @e2 "password"
agent-browser click @e3
agent-browser wait --url "**/issues"
agent-browser screenshot --full dashboard.png
```

### Responsive Testing

```bash
# Test mobile viewport
agent-browser open http://localhost:3000/issues --viewport 375x812
agent-browser screenshot --full mobile.png

# Test tablet viewport
agent-browser open http://localhost:3000/issues --viewport 768x1024
agent-browser screenshot --full tablet.png

# Test desktop viewport
agent-browser open http://localhost:3000/issues --viewport 1440x900
agent-browser screenshot --full desktop.png
```

### Video Recording (if supported)

```bash
agent-browser record start validation.webm
# ... walk through the UI flow ...
agent-browser record stop
multica attachment upload validation.webm --issue <issue-id>
```

## When to Use This Skill

- **Always** when your changes touch `.tsx`, `.css`, or any UI-related files
- **Always** when the issue description mentions visual changes, layout, or design
- **Skip** for pure backend changes, config changes, or non-visual refactors
