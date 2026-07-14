# Concepts

Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

---

## Core Entities

### Skill
An agent-loadable instruction set that augments an AI agent's capabilities. Skills are authored as structured documents with metadata, imported from runtimes into workspaces, and can be assigned to agents. Skills have a `root` field indicating their discovery source (provider vs universal), enabling grouping and provenance tracking.
*In Chinese UI copy: keep "skill" in English, do not translate.*

### Runtime
A daemon-managed execution environment that exposes local skills for import. Runtimes are polled for available skills (typically every 500ms with a 30s timeout) and provide the source from which skills are copied into a workspace. Each runtime has a unique ID and status (online/offline).

### Daemon
The local agent process that manages runtime lifecycle, skill discovery, and agent execution. Daemons run on developer machines and expose skills through a discovery API that the backend polls. Older daemons may omit the `root` field on skill records.

---

## Status Concepts

### Root
A skill's discovery source classification. Values: `provider` (runtime's own skill directory, e.g., `~/.claude/skills`), `universal` (cross-tool fallback directory, e.g., `~/.agents/skills`), or `undefined` (older daemons). Used for grouping skills in search interfaces. Skills with undefined root are bucketed into an "Other" group rather than dropped.

### Branch (UI)
A conditional rendering path in adaptive UI components. Computed directly from data (e.g., item count) in render, never synchronized via state + effect. Pattern: `branch = data.length === 0 ? "empty" : data.length <= 2 ? "summary" : "search"`. Ensures UI stays in lockstep with data and avoids transient off-by-one render bugs.

---

## Processes

### Skill Import
The act of copying a skill from a runtime into a workspace. Imports can be single (one skill) or bulk (multiple skills). Bulk imports may encounter name conflicts, which are resolved via overwrite, rename, or skip decisions. The import flow preserves selection state across UI branch switches when the dialog remains open.

---

## Agent Access

### AccessScope
The three-state model describing who can invoke (trigger) an agent: **workspace** (any workspace member, plus internal agents and system triggers), **specific-people** (only named members or teams designated as invocation targets), or **owner-only** (only the agent's owner). Derived from two backend fields — `permission_mode` ("private" or "public_to") and `invocation_targets` (workspace/member/team targets) — but is the operator-facing conceptual model displayed in the agents list UI. The legacy `visibility` field is a lossy two-state projection of this model (maps both "specific-people" and "owner-only" to "private"), so UI surfaces should use the three-state AccessScope, not the derived `visibility`, to avoid misleading operators.

---

## Relationships

- A **Runtime** exposes many **Skills**, each with a **Root** classification.
- A **Daemon** manages one or more **Runtimes** on a local machine.
- **Skill Import** transfers a **Skill** from a **Runtime** into a workspace.
- **Branch (UI)** determines which rendering path is shown based on skill count in a list dialog.
