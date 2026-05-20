# Multica Fork — Browser Regression Test Guide

## Overview

This guide describes how to run browser regression tests for the Multica Fork features. The testcases are located in `testcase/case/tc-*.md` and each is written in natural-language format suitable for agent-browser automation.

## Authentication

All browser tests use the fixed verification code login:
- **Email**: `tester@multica.com`
- **Code**: `888888`
- **Auth fixture**: `testcase/auth/auth.json`

The login flow is documented in `tc-001-fixed-verification-code-login.md`. Execute this first to establish a session.

## Test Environment Requirements

- Multica web app running (self-hosted or development instance)
- Backend with `APP_ENV≠production` (enables fixed verification code)
- SMTP configured (for email notification tests)
- DingTalk bot configured (for DingTalk notification tests, optional)
- Google OAuth configured (for Google login tests, optional)
- At least one agent with a working runtime
- At least one workspace with multiple members (for permission tests)

## Testcase Categories

### Authentication (tc-001, tc-005, tc-006)
- Fixed verification code login (core, must pass)
- DingTalk OAuth login (requires DingTalk sandbox)
- Google OAuth login (requires Google credentials)

### Notifications (tc-002, tc-003, tc-007, tc-008, tc-037, tc-040, tc-046, tc-047)
- Custom webhook create & test
- Custom webhook channel toggle
- DingTalk notification binding & delivery
- Email notification binding & delivery
- Webhook custom message format
- OpenClaw WeChat notification channel binding and per-event toggles
- Notification render mode Settings controls
- IM compact task notification delivery for WeChat and DingTalk

### Agent Management (tc-009 to tc-012, tc-036)
- Mine/All agent filter
- Agent duplicate with env masking
- Agent permission controls (owner vs member)
- Agent Defaults (system & personal)
- DeepSeek TUI runtime integration

### Issue Features (tc-004, tc-015 to tc-020, tc-024, tc-026, tc-027, tc-029 to tc-032)
- Auto-set project on new issue
- Clear issue history
- Comment draft preservation
- @mention agent filtering with recent priority
- Execution log enhancements (run index, filter, coloring)
- Retry agent comment
- Issue delete permission restriction
- Issue identifier routes
- Issue scroll controls
- Auto-status on comment
- My Issues includes unassigned
- Comment copy link
- Auto-block on task fail
- Timeline cycle resilience

### Autopilot (tc-021)
- Run row enhancements (duration, output, expand/collapse)

### Chat (tc-022, tc-034)
- Message delete & retry with rate-limit auto-retry
- Private chat session ownership gate

### Wiki (tc-013)
- Multi-page wiki with auto-save and attachments

### Invite (tc-014)
- Invite link generation and acceptance flow

### Skills (tc-023)
- Batch upload including dot directories

### Projects (tc-025)
- Project sorting

### Mobile (tc-033, tc-043 to tc-045, tc-057)
- Core mobile app functionality (login, issues, search, i18n)
- Mobile issue detail labels/start date/parent issue editing
- Mobile inbox batch actions
- Mobile issue list label filtering
- Mobile issue detail comments and timeline

### CLI (tc-035)
- Managed update flow with OBS manifest `download_url` + `checksum`; GitHub Release is fallback only

### Plan Mode (tc-028)
- Native Claude plan mode with approval bridge

### Editor (tc-038)
- Paste image upload with error feedback

### Comment UX (tc-039)
- Comment body collapse/expand for long comments (OPE-700)

### WeChat Notification (tc-040)
- OpenClaw WeChat notification channel binding and per-event toggles (OPE-544)

### Notification Rendering (tc-046, tc-047)
- Notification render mode Settings controls
- IM compact task notification delivery for WeChat and DingTalk

### Wiki Extended (tc-041)
- Wiki page creator display and lightweight activity log (OPE-843)

### Issue Subscription (tc-042)
- Subscribe/unsubscribe toggle with subscriber list sorted by subscribed-first (OPE-995)

### Mobile — Issue Properties (tc-043)
- Mobile issue detail: edit labels, start date, parent issue

### Mobile — Inbox Batch (tc-044)
- Mobile inbox batch mark-as-read and batch archive

### Mobile — Label Filter (tc-045)
- Mobile issue list label filtering

## Execution Order

1. **tc-001** (login) — establishes session, required for all others
2. **tc-004, tc-024, tc-026, tc-029** — basic issue features
3. **tc-009 to tc-012** — agent management
4. **tc-002, tc-003, tc-037** — webhook features
5. **tc-013 to tc-016** — wiki, invite, clear history, drafts
6. **tc-017 to tc-020** — advanced issue features
7. **tc-021 to tc-023** — autopilot, chat, skills
8. **tc-025 to tc-028** — projects, scroll, auto-status, plan mode
9. **tc-030 to tc-032** — copy link, auto-block, timeline resilience
10. **tc-033 to tc-038** — mobile, private chat, CLI manifest update, DeepSeek, paste
11. **tc-039 to tc-042** — comment collapse, WeChat notification, wiki activity, subscription
12. **tc-043 to tc-045, tc-057** — mobile issue properties, inbox batch, label filter, mobile comments/timeline
13. **tc-046 to tc-047** — notification render mode settings and IM compact task delivery

## Notes

- Testcases are written in natural language targeting visible UI elements (labels, button text, headings), not CSS selectors or data-testid attributes.
- Each testcase is self-contained with its own preconditions.
- For tests requiring multiple users, use separate browser sessions or incognito windows.
- External OAuth tests (DingTalk, Google) may be skipped in CI environments without credentials.
- The `testcase/auth/auth.json` file provides the default test account credentials.
