---
title: Skills
description: Attach "knowledge packs" to an agent — compatible with the Anthropic Agent Skills open standard.
---

import { Callout } from "fumadocs-ui/components/callout";

A Skill is a **knowledge pack** for an [agent](/agents) — a `SKILL.md` plus optional supporting files (scripts, configs, reference templates) that tell the agent "when you hit this kind of task, think and act like this." Multica adopts the [Anthropic Agent Skills](https://agentskills.io) open standard, so any compliant Skill — from Anthropic's official repository, ClawHub, or skills.sh — can be imported directly.

## Workspace skills vs. local skills

Multica supports two skill sources:

- **Workspace skill** — stored in Multica's cloud. Once attached to an agent, it's synced down to your daemon at task execution time. This is the **standard way to share skills across a team**.
- **Local skill** — lives in a directory on your machine (each AI coding tool has a conventional default path, e.g. Claude Code's `~/.claude/skills/`). On your request, the [daemon](/daemon-runtimes) scans your machine, and you manually pick which ones to bring into the workspace.

Most of the time you want **workspace skills**: import once, every teammate's agent can use it. Local skills are a fit when you want to test locally first, or when the content involves sensitive local material.

## Importing a skill

Workspace skills come from four sources:

- **New** — write the `SKILL.md` and related files directly in the UI
- **From GitHub** — paste a repo URL (e.g. `https://github.com/owner/repo/tree/main/skills/my-skill`) and Multica pulls the `SKILL.md` and every file in that directory
- **From ClawHub** — search and import from the [ClawHub](https://clawhub.io) public marketplace, with version selection
- **From local** — the daemon scans skill directories on your machine, and you pick which to bring into the workspace

Both individual files and whole skill packs have size caps (single-file cap around 1 MB when importing from GitHub). The exact rules appear in the import dialog — exceeding them returns an error.

## Attaching to an agent

Once imported, a skill has to be **attached to a specific agent** to take effect. One agent can have multiple skills attached, and one skill can be attached to multiple agents.

After attaching, the agent picks up its skills the next time it starts a task — each AI coding tool has its own skill discovery path (Claude Code uses `.claude/skills/`, Cursor uses `.cursor/skills/`, etc.), and Multica drops files in the right place automatically. **However, three tools (Gemini, Hermes, OpenClaw) currently use the generic fallback path `.agent_context/skills/` — whether these tools actually read skills from that path depends on the tool itself.** Full path mapping and the native-discovery vs. fallback distinction is in [AI coding tools comparison → Where skill files go](/providers#where-skill-files-go).

After you edit a skill's contents, **only newly created tasks pick up the new version** — tasks already running continue with the old skill.

## Safety of third-party skills

Skills imported from GitHub or ClawHub may include scripts and executable content. Multica itself **does not sign, audit, or sandbox them** — skill contents are handed to the corresponding AI coding tool as-is, and whether the tool treats them as executable is up to the tool.

<Callout type="warning">
**Before importing a third-party skill, review the `SKILL.md` and every file that ships with it.**

In February 2026 the "ClawHavoc" incident — malicious instructions planted in a popular skill pack stole API keys from affected users. ClawHub has since added VirusTotal scanning, but **automated scans are not a substitute for your own review**.

**Only import from sources you trust.** For projects involving sensitive data, consider using only local skills you wrote yourself.
</Callout>

## Skills vs. MCP

Both augment what an agent can do, but in different directions:

- **Skill** = a structured **knowledge pack** (static content + instructions). The agent reads a skill to learn "when I see problem X, here's how to think and what to do."
- **MCP** (Model Context Protocol) = a **tool channel**. The agent uses MCP to connect to external services (databases, filesystems, third-party APIs) and **invoke** them.

The two are complementary. In Multica today, MCP support is **only truly consumed by Claude Code** — other tools receive the MCP config but don't actually use it. A dedicated MCP section will come in a later release.

---

By now you know what an agent is, how to create one, and how to attach skills. The next question: **where does it actually run, and why does my agent sometimes get stuck?** The next chapter covers the execution architecture — daemons, runtimes, and how tasks work together.

## Next steps

- [Daemon and runtimes](/daemon-runtimes) — where agents actually run, and how to tell online from offline
- [Executing tasks](/tasks) — the full lifecycle of one "agent work session"
- [AI coding tools comparison](/providers) — full comparison of all 10 tools (including each one's skill injection path)
