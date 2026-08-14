# Multica Operator Design

## Summary

Multica Operator is a portable Agent Skill that lets Codex, Claude Code, and
other Agent Skills hosts operate Multica through the existing community
`multica` CLI. It supports direct resource operations and higher-level business
workflow design without adding private CLI commands or bypassing the CLI with
HTTP requests.

The repository contains one canonical Skill source. A single build script
generates the Codex Marketplace plugin, Claude Marketplace plugin, and GitHub
Release archive from that source.

## Scope

The Skill supports:

- silent CLI presence checks, automatic official CLI installation,
  browser-based login/setup, and workspace discovery;
- direct operations on known Multica resources;
- decomposition of business goals into Issues, Projects, Agents, Squads,
  Skills, and Autopilots;
- conservative reuse or creation decisions based on resource sharing risk;
- complete-plan confirmation, dependency-ordered execution, verification, and
  resumable failure reporting;
- host-managed installation and update through Codex Marketplace, Claude
  Marketplace, or a GitHub Release archive.

The Skill does not modify the community CLI, call Multica APIs directly, create
or edit Skill definitions, manage its own installation, or promise support for
commands absent from the installed CLI.

## Community CLI Boundary

The installed community CLI is the execution and compatibility boundary. The
Skill may inspect local command help and use structured CLI output, but it must
not read profile tokens or invoke `curl` or a generic HTTP client as an API
fallback. When an operation has no CLI command, the Skill labels it as a
Multica Web step and waits for that step before continuing dependent work.

The safe connection proof is:

```bash
multica workspace list --output json
```

Valid JSON proves workspace access in the selected command context. An empty
array is a successful connection with no accessible workspace. The community
CLI does not expose a safe structured identity/status command, so the Skill
does not claim account identity or the effective server unless the user
supplied it explicitly. Plain-text auth status is avoided because older CLI
versions may expose token material.

The Skill preserves an explicitly selected profile and server on subsequent
commands. Login may open a browser, wait for workspace creation, or select a
default according to community CLI behavior. Credentials and verification
codes remain in the browser or CLI prompt.

An explicit request to connect, sign in, or set up Multica authorizes installing
the missing community CLI through the official installer for the detected host.
The Operator runs the installation without a separate confirmation, verifies it
with `multica version`, and stops on installer failure or when the command remains
unavailable. It does not silently upgrade or replace an existing CLI. Host-level
approval prompts, such as administrator authorization, remain visible to the
user.

CLI installation does not authorize choosing a Multica deployment. When no
usable local target exists and the user did not provide one, the Operator asks
the user to choose Multica Cloud or a self-hosted deployment before running
`setup` or `login`. Cloud uses the documented official URLs. Self-hosted setup
requires the exact Server and Apps URLs from the user; the Operator never derives
one from the other. An existing configured target or an explicit user-supplied
target is preserved without asking the same question again.

Connection setup is quiet by default. The Operator does not narrate Skill file
reads, repository instructions, version checks, CLI presence checks, installer
selection, or other internal routing. A failed non-blocking Skill version lookup
is silent. It surfaces only user interaction that cannot be completed on the
user's behalf, a material choice such as selecting a deployment or workspace,
an observed failure, or the final verified workspace result. Before browser
authentication, it gives one concise notice that credentials and verification
remain in the browser.

## Request Routing

A concrete action on a known target uses the direct-operation path: inspect the
target, preview the mutation, execute the confirmed scope, and verify the CLI
result.

An open-ended outcome, business-orchestration question, resource-selection
question, or set of dependent mutations uses orchestration. This is operational
design for executing work in Multica, not software design. The Operator
clarifies only facts that materially affect the plan, queries relevant
resources, and classifies the work as one-time, recurring, or coordinated.

Whenever the Operator presents two or more mutually exclusive choices, it uses
a numbered list and accepts a number-only reply as the selected displayed
option. It asks again only for an invalid or ambiguous selection.

Resource choice follows observed sharing risk:

- dedicated resources may be adjusted when evidence is sufficient;
- shared resources remain unchanged and an isolated resource is preferred;
- unknown resources are treated as shared;
- existing matching resources are preferred over duplicates.

Squads are used only when coordination or role separation adds value.
Autopilots assign Agents rather than Squads; recurring coordinated work is
assigned to the Squad leader Agent.

## Skill And Agent Safety

The Operator may inspect and bind an existing Skill but does not create,
update, import, or delete Skill definitions. A missing capability may be
represented temporarily as an explicitly marked embedded instruction in an
Issue or Autopilot description, but only when the chosen Agent already has the
required tools, permissions, credentials, and data access. Recurring temporary
instructions include a reminder to promote stable rules into a maintained
Skill.

Agent configuration changes are treated as high impact. The complete plan must
show the exact Agent or Skill-binding delta without secrets, and a separate
confirmation is required immediately before an Agent create or update. Merely
assigning an unchanged Agent to an Issue or adding it to a Squad is covered by
the plan-level confirmation.

## Planning And Execution

Before writing state, the Operator presents one complete plan containing the
goal, acceptance criteria, decomposition, dependencies, resource choices,
sharing evidence, all mutations, temporary instructions, Web-only steps,
risks, and long-term recommendations. One confirmation authorizes only the
listed mutations in the stated workspace.

The plan presented in chat is both the orchestration design and the execution
plan. After the user confirms it, the Operator executes it directly in
dependency order. It does not automatically create a repository design
document, specification, or software implementation plan, and it does not ask
the user to review the same plan again in a file. Those artifacts are created
only when the user explicitly requests them and do not become another approval
gate unless requested.

Execution follows dependency order. Passive dependencies and backlog Issues
are prepared first; an Autopilot is created only after its Agent, Project, and
prompt are ready; triggers are enabled last. Returned IDs become the identity
for dependent commands and resume checks.

If observed state differs materially from the approved plan, execution pauses
before the changed step. On command failure, dependent steps stop and the
Operator reports completed resources, verified IDs, the observed error, steps
not run, and the exact resume point. Resume verifies recorded IDs and never
recreates resources by name.

## Distribution Architecture

`skills/multica-operator/` is the only editable Skill source. The command:

```bash
bash scripts/build-operator-distribution.sh X.Y.Z <empty-output-dir>
```

copies that source once, injects `VERSION`, includes the repository license,
rejects links and special files, and produces:

```text
marketplace/
  .agents/plugins/marketplace.json
  .claude-plugin/marketplace.json
  plugins/multica-operator/
  release.json
  LICENSE
release/
  multica-operator-X.Y.Z.tar.gz
  multica-operator-X.Y.Z.tar.gz.sha256
```

The two Marketplace manifests reference the same generated plugin payload.
The GitHub archive contains the same staged Skill content. Cross-platform
archive bytes need not be identical; content correctness and the emitted
SHA-256 are the contract.

## Release Semantics

Stable `vX.Y.Z` tags publish the Operator after the normal release job. The
workflow has explicit `contents: write` permission and serializes Marketplace
updates.

GitHub Release assets are an atomic pair. If both archive and checksum are
absent, both are uploaded. If both exist, rerun succeeds without mutation. If
only one exists, publication fails for manual repair. Existing assets are never
overwritten with `--clobber`.

The Marketplace branch is updated last. Its `release.json` version must be
valid stable semver. An older tag exits successfully without changing a newer
Marketplace. An equal version with identical generated content is a successful
no-op; equal version with different content fails. A newer version replaces the
generated Marketplace tree in one commit.

## Verification

The public Skill contract test validates required files, routing anchors, CLI
boundaries, portable frontmatter, automatic official installation, explicit
deployment selection, and quiet connection behavior. The unified distribution
test runs the builder against valid and invalid inputs, compares Marketplace and
archive Skill content, validates manifests, and checks the checksum. Release
workflow syntax and repository diff checks run before commit.

The final pull request must contain no net changes to community CLI source,
CLI tests, or CLI documentation. It contains no handoff, execution plan,
temporary review, or superseded design document.
