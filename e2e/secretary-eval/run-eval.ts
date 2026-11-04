#!/usr/bin/env node
/**
 * Secretary evaluation runner (WS13, spec 13.8.1).
 *
 * Seeds the company in scenarios.json through the API, runs each scenario, and writes the
 * outputs and a score sheet into e2e/secretary-eval/results/<stamp>-<mode>/. The checks are
 * in rubric.md. Scoring a model's answer is a manual step, except the checks marked [auto].
 *
 *   node e2e/secretary-eval/run-eval.ts --dry-run          no network: validates the set and the rubric
 *   node e2e/secretary-eval/run-eval.ts                    --mode plumbing (default): scripted leaders, no model
 *   SECRETARY_EVAL_LIVE=1 node e2e/secretary-eval/run-eval.ts --mode live
 *
 * Options: --scenario <id> (repeatable), --keep (do not delete the seeded data).
 *
 * Environment:
 *   MULTICA_API                  default http://localhost:8080
 *   MULTICA_TOKEN                personal access token (mul_...) of an owner or admin
 *   MULTICA_WORKSPACE_ID         the evaluation workspace
 *   SECRETARY_EVAL_RUNTIME_ID    runtime the evaluation leaders are created on
 *   SECRETARY_EVAL_SECRETARY_ID  the workspace's Secretary agent
 *   SECRETARY_EVAL_LIVE=1        required for --mode live; the runner refuses without it
 *   SECRETARY_EVAL_TIMEOUT_MS    live mode: longest wait for the Secretary (default 30 minutes)
 *
 * Plumbing mode pauses the company before it seeds anything, so a leader or Secretary run it
 * triggers is held and no model is called, even with a daemon attached (company pause holds
 * leader runs, spec 4.5). It restores the previous state at the end. It never sends a chat
 * message. Run it with Node 24 or later, which strips the types natively.
 */
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");
const skillsDir = join(repoRoot, "packages/core/squads/skills");
const resultsRoot = join(here, "results");

// ---- Scenario file shape -------------------------------------------------------

interface SquadFact {
  title: string;
  status: string;
  note: string;
}

interface Squad {
  key: string;
  name: string;
  leader: string;
  mission: string;
  issues: SquadFact[];
  comments: { on: string; by: string; text: string }[];
  note?: string;
  runs?: string;
  goal?: unknown;
}

interface Scenario {
  id: string;
  title: string;
  boss_question: string;
  squads: Squad[];
  scripted_plan: { ask: string[]; skip: { key: string; reason: string }[] };
  scripted_leader_answers: Record<string, string>;
  expect: {
    must_mention: string[];
    must_not_say: string[];
    expected_addressees: string[];
    forbidden_addressees: string[];
    expected_skip: string[];
    challenge_required: boolean;
    max_words?: number;
    properties: string[];
  };
}

interface ScenarioFile {
  version: number;
  default_max_words: number;
  scenarios: Scenario[];
}

// ---- API shapes the runner reads (only the fields it uses) ---------------------

interface Named {
  id: string;
}

interface IssueRow {
  id: string;
  title: string;
  status?: string;
  parent_issue_id?: string | null;
  assignee_type?: string | null;
  assignee_id?: string | null;
  created_at?: string;
}

interface CommentRow {
  id: string;
  author_type: string;
  author_id: string;
  content: string;
  created_at: string;
}

interface ChatMessage {
  id: string;
  role: string;
  content: string;
  created_at: string;
}

interface CompanyState {
  /** The state in force now. */
  state: string;
  /** The state the boss set; a quiet window does not change it. */
  setting: string;
}

// ---- Helpers -------------------------------------------------------------------

const internalTerms = ["child_done", "wakeup", "origin_id", "squad_request", "chat_session_id"];

const exitWith = (message: string, code = 1): never => {
  console.error(message);
  process.exit(code);
};

function parseArgs(argv: string[]) {
  const args = { dryRun: false, mode: "plumbing" as "plumbing" | "live", scenarios: [] as string[], keep: false };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--dry-run") args.dryRun = true;
    else if (arg === "--keep") args.keep = true;
    else if (arg === "--mode") {
      const value = argv[++i];
      if (value === "live") args.mode = "live";
      else if (value === "plumbing") args.mode = "plumbing";
      else exitWith(`--mode must be plumbing or live, got ${value}`, 2);
    } else if (arg === "--scenario") {
      const value = argv[++i];
      if (!value) exitWith("--scenario needs an id", 2);
      args.scenarios.push(value);
    } else exitWith(`unknown option ${arg}`, 2);
  }
  return args;
}

function loadScenarios(): ScenarioFile {
  return JSON.parse(readFileSync(join(here, "scenarios.json"), "utf8")) as ScenarioFile;
}

/** Checks the scenario file's internal references. Throws on the first problem. */
function validate(file: ScenarioFile) {
  const ids = new Set<string>();
  for (const sc of file.scenarios) {
    const where = `scenario ${sc.id}`;
    if (ids.has(sc.id)) throw new Error(`${where}: duplicate id`);
    ids.add(sc.id);
    const keys = new Set(sc.squads.map((s) => s.key));
    if (keys.size !== sc.squads.length) throw new Error(`${where}: duplicate squad key`);
    for (const key of [...sc.scripted_plan.ask, ...sc.expect.expected_addressees, ...sc.expect.forbidden_addressees]) {
      if (!keys.has(key)) throw new Error(`${where}: unknown squad key ${key}`);
    }
    for (const key of sc.scripted_plan.ask) {
      if (!sc.scripted_leader_answers[key]) throw new Error(`${where}: no scripted leader answer for ${key}`);
    }
    for (const key of sc.expect.expected_addressees) {
      if (sc.expect.forbidden_addressees.includes(key)) throw new Error(`${where}: ${key} is both expected and forbidden`);
    }
  }
}

/** The rubric's check ids, read from rubric.md, e.g. R1a, R4a. */
function rubricChecks(): { id: string; text: string; auto: boolean }[] {
  // A check may wrap: continuation lines are indented by two spaces.
  const checks: { id: string; text: string; auto: boolean }[] = [];
  for (const line of readFileSync(join(here, "rubric.md"), "utf8").split("\n")) {
    const match = /^- (R\d[a-c])\. (.*)$/.exec(line);
    if (match) {
      checks.push({ id: match[1], text: match[2], auto: false });
    } else if (line.startsWith("  ") && checks.length > 0) {
      checks[checks.length - 1].text += ` ${line.trim()}`;
    }
  }
  for (const check of checks) check.auto = check.text.includes("**[auto]**");
  return checks;
}

function hashSkills(): Record<string, { path: string; sha256: string }> {
  const out: Record<string, { path: string; sha256: string }> = {};
  for (const name of readdirSync(skillsDir, { withFileTypes: true }).filter((e) => e.isDirectory()).map((e) => e.name).sort()) {
    for (const file of ["SKILL.md", "reference.md"]) {
      const path = join(skillsDir, name, file);
      if (!existsSync(path)) continue;
      const rel = path.slice(repoRoot.length + 1);
      out[rel] = { path: rel, sha256: createHash("sha256").update(readFileSync(path)).digest("hex") };
    }
  }
  return out;
}

function stampNow(): string {
  return new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
}

function writeJson(path: string, value: unknown) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
}

function writeText(path: string, value: string) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, value.endsWith("\n") ? value : `${value}\n`);
}

// ---- API client (documented HTTP; no fixtures import) --------------------------

class Api {
  private readonly base: string;
  private readonly token: string;
  private readonly workspaceId: string;

  constructor(base: string, token: string, workspaceId: string) {
    this.base = base;
    this.token = token;
    this.workspaceId = workspaceId;
  }

  async call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(`${this.base}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${this.token}`,
        "X-Workspace-ID": this.workspaceId,
        "Content-Type": "application/json",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    if (!res.ok) throw new Error(`${method} ${path} -> ${res.status}: ${text.slice(0, 300)}`);
    return (text ? JSON.parse(text) : undefined) as T;
  }
}

interface Cleanup {
  issues: string[];
  squads: string[];
  agents: string[];
}

// ---- Seeding -------------------------------------------------------------------

interface SeededSquad {
  id: string;
  name: string;
}

interface Seeded {
  squads: Map<string, SeededSquad>;
}

async function seedCompany(api: Api, sc: Scenario, runtimeId: string, cleanup: Cleanup): Promise<Seeded> {
  const squads = new Map<string, SeededSquad>();
  for (const squad of sc.squads) {
    const leader = await api.call<Named>("POST", "/api/agents", { name: squad.leader, runtime_id: runtimeId });
    cleanup.agents.push(leader.id);
    const created = await api.call<Named>("POST", "/api/squads", {
      name: squad.name,
      description: squad.mission,
      leader_id: leader.id,
    });
    cleanup.squads.push(created.id);
    squads.set(squad.key, { id: created.id, name: squad.name });

    // Facts are plain issues, unassigned, so seeding starts no run.
    const byTitle = new Map<string, Named>();
    const facts: { title: string; status: string; description: string }[] = squad.issues.map((i) => ({
      title: i.title,
      status: i.status,
      description: i.note,
    }));
    if (squad.note || squad.runs) {
      facts.push({ title: `${squad.name}: scripted facts`, status: "in_progress", description: [squad.note, squad.runs].filter(Boolean).join("\n") });
    }
    if (squad.goal) {
      facts.push({ title: `${squad.name}: goal`, status: "in_progress", description: JSON.stringify(squad.goal, null, 2) });
    }
    for (const fact of facts) {
      const issue = await api.call<Named>("POST", "/api/issues", { ...fact, priority: "medium" });
      cleanup.issues.push(issue.id);
      byTitle.set(fact.title, issue);
    }
    for (const comment of squad.comments) {
      const target = byTitle.get(comment.on);
      if (!target) throw new Error(`comment on unknown issue "${comment.on}" in ${sc.id}`);
      await api.call("POST", `/api/issues/${target.id}/comments`, { content: comment.text });
    }
  }
  return { squads };
}

// ---- Plumbing mode -------------------------------------------------------------

interface WiringCheck {
  check: string;
  pass: boolean;
}

async function runPlumbing(api: Api, sc: Scenario, runtimeId: string, secretaryId: string, cleanup: Cleanup, dir: string) {
  const seeded = await seedCompany(api, sc, runtimeId, cleanup);

  // The request, as the Secretary would file it. The company is paused, so the assignments hold.
  const parent = await api.call<IssueRow>("POST", "/api/issues", {
    title: `Eval request: ${sc.boss_question}`,
    description: sc.boss_question,
    status: "todo",
    priority: "medium",
    assignee_type: "agent",
    assignee_id: secretaryId,
  });
  cleanup.issues.push(parent.id);

  const children = new Map<string, IssueRow>();
  for (const key of sc.scripted_plan.ask) {
    const squad = seeded.squads.get(key);
    if (!squad) throw new Error(`no seeded squad for ${key}`);
    const child = await api.call<IssueRow>("POST", "/api/issues", {
      title: `Ask ${squad.name}: ${sc.boss_question}`,
      description: "Scripted plumbing ask. The leader's answer below is scripted, not a model's.",
      status: "todo",
      priority: "medium",
      parent_issue_id: parent.id,
      assignee_type: "squad",
      assignee_id: squad.id,
    });
    cleanup.issues.push(child.id);
    children.set(key, child);
    await api.call("POST", `/api/issues/${child.id}/comments`, { content: sc.scripted_leader_answers[key] });
    await api.call("PUT", `/api/issues/${child.id}`, { status: "done" });
  }

  // Wiring checks: the plumbing the spec asks for, not the Secretary's judgment.
  const checks: WiringCheck[] = [];
  const reread = await api.call<IssueRow>("GET", `/api/issues/${parent.id}`);
  checks.push({ check: "parent assigned to the Secretary", pass: reread.assignee_id === secretaryId });
  for (const key of sc.expect.expected_addressees) {
    checks.push({ check: `child for ${key}`, pass: children.has(key) });
  }
  for (const key of sc.expect.forbidden_addressees) {
    checks.push({ check: `no child for ${key}`, pass: !children.has(key) });
  }
  for (const [key, child] of children) {
    const got = await api.call<IssueRow>("GET", `/api/issues/${child.id}`);
    checks.push({ check: `child ${key} carries the parent and is done`, pass: got.parent_issue_id === parent.id && got.status === "done" });
  }

  writeJson(join(dir, "plan.json"), sc.scripted_plan);
  writeJson(join(dir, "wiring.json"), { parent: parent.id, checks });
  for (const [key, child] of children) {
    const comments = await api.call<CommentRow[]>("GET", `/api/issues/${child.id}/comments`);
    writeJson(join(dir, `leader-${key}.json`), comments);
  }
  return { checks };
}

// ---- Live mode -----------------------------------------------------------------

interface LiveResult {
  assessment: string;
  messages: ChatMessage[];
  created: IssueRow[];
  challengeComments: number;
  wiringPass: boolean;
  forbiddenHit: string[];
}

async function runLive(api: Api, sc: Scenario, runtimeId: string, secretaryId: string, cleanup: Cleanup, dir: string, timeoutMs: number, seeded: Seeded): Promise<LiveResult> {
  const session = await api.call<Named>("POST", "/api/chat/sessions", {
    agent_id: secretaryId,
    title: `Eval ${sc.id}`,
  });
  const startedAt = Date.now();
  await api.call("POST", `/api/chat/sessions/${session.id}/messages`, { content: sc.boss_question });

  // Wait until the Secretary has said something and then gone quiet for five minutes, or time runs out.
  let messages: ChatMessage[] = [];
  let lastCount = -1;
  let quietSince = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    await new Promise((r) => setTimeout(r, 15000));
    messages = await api.call<ChatMessage[]>("GET", `/api/chat/sessions/${session.id}/messages`);
    const assistant = messages.filter((m) => m.role === "assistant");
    if (assistant.length !== lastCount) {
      lastCount = assistant.length;
      quietSince = Date.now();
    }
    if (assistant.length > 0 && Date.now() - quietSince > 5 * 60 * 1000) break;
  }
  const assistant = messages.filter((m) => m.role === "assistant");
  const assessment = assistant.length > 0 ? assistant[assistant.length - 1].content : "";

  // Issues the Secretary created during this run, found by creation time. The list is bounded.
  const listing = await api.call<IssueRow[] | { issues: IssueRow[] }>("GET", "/api/issues?limit=200");
  const all = Array.isArray(listing) ? listing : listing.issues;
  const created = all.filter((i) => i.created_at !== undefined && Date.parse(i.created_at) >= startedAt);

  const forbiddenIds = sc.expect.forbidden_addressees.map((k) => seeded.squads.get(k)?.id).filter((x): x is string => !!x);
  const forbiddenHit = created.filter((i) => forbiddenIds.includes(i.assignee_id ?? "")).map((i) => i.title);

  let challengeComments = 0;
  for (const issue of created) {
    if (!issue.parent_issue_id) continue;
    const comments = await api.call<CommentRow[]>("GET", `/api/issues/${issue.id}/comments`);
    challengeComments += comments.filter((c) => c.author_type === "agent" && c.author_id === secretaryId).length;
  }

  writeText(join(dir, "assessment.md"), assessment || "(no assistant message before the timeout)");
  writeJson(join(dir, "chat.json"), messages);
  writeJson(join(dir, "created-issues.json"), created);
  return {
    assessment,
    messages,
    created,
    challengeComments,
    wiringPass: forbiddenHit.length === 0,
    forbiddenHit,
  };
}

// ---- Score sheet ---------------------------------------------------------------

function autoCell(id: string, sc: Scenario, live: LiveResult | null): string {
  if (!live) return "-";
  const text = live.assessment;
  const words = text.trim().split(/\s+/).filter(Boolean).length;
  const maxWords = sc.expect.max_words ?? 350;
  switch (id) {
    case "R1b":
      return sc.expect.must_mention.every((t) => text.toLowerCase().includes(t.toLowerCase())) ? "yes" : "no";
    case "R4a":
      return /\]\(|https?:\/\/|\/issues\/|\b[A-Z]{2,}-\d+\b/.test(text) ? "yes" : "no";
    case "R5a":
      return sc.expect.expected_skip.every((n) => text.includes(n) && /skip|not asked|left out|did not ask/i.test(text)) ? "yes" : "no";
    case "R5b":
      return live.wiringPass ? "yes" : "no";
    case "R6b":
      return sc.expect.challenge_required ? (live.challengeComments > 0 ? "yes" : "no") : "N/A";
    case "R7a":
      return `${words <= maxWords ? "yes" : "no"} (${words} words)`;
    case "R8b":
      return internalTerms.some((t) => text.includes(t)) ? "no" : "yes";
    default:
      return "manual";
  }
}

function scoreSheet(sc: Scenario, mode: string, checks: { id: string; text: string; auto: boolean }[], live: LiveResult | null, wiring: WiringCheck[] | null): string {
  const lines: string[] = [];
  lines.push(`# Score sheet: ${sc.id}`, "");
  lines.push(`Mode: ${mode}. ${sc.title}.`, "");
  lines.push(`Boss question: ${sc.boss_question}`, "");
  if (mode === "plumbing") {
    lines.push("**Plumbing mode: no Secretary output exists.** Every rubric check below is manual and cannot be scored from this run. The wiring checks are the only evidence here.", "");
    lines.push("| Wiring check | Pass |", "|---|---|");
    for (const w of wiring ?? []) lines.push(`| ${w.check} | ${w.pass ? "yes" : "no"} |`);
    lines.push("");
  } else if (live) {
    const hits = sc.expect.must_not_say.filter((t) => live.assessment.toLowerCase().includes(t.toLowerCase()));
    lines.push(`Words in the assessment: ${live.assessment.trim().split(/\s+/).filter(Boolean).length}. Terms from must_not_say found (check R2c and R6c by hand): ${hits.length ? hits.join(", ") : "none"}.`, "");
    lines.push(`Child issues created during the run: ${live.created.length}. Challenge comments by the Secretary: ${live.challengeComments}.`, "");
  }
  lines.push("## Properties this scenario tests", "");
  for (const p of sc.expect.properties) lines.push(`- ${p}`);
  lines.push("", "## Checks", "", "| Check | Auto | Score by hand (yes or no) | Note |", "|---|---|---|---|");
  for (const check of checks) {
    lines.push(`| ${check.id}. ${check.text} | ${autoCell(check.id, sc, live)} | | |`);
  }
  lines.push("", "Dimension score: 2 if every applicable check is yes, 1 if some are, 0 if none, N/A if the scenario does not test it. Total: ____ of ____.", "");
  return lines.join("\n");
}

// ---- Main ----------------------------------------------------------------------

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.mode === "live" && process.env.SECRETARY_EVAL_LIVE !== "1") {
    exitWith("Refusing to call a real model: --mode live needs SECRETARY_EVAL_LIVE=1 in the environment.", 2);
  }

  const file = loadScenarios();
  validate(file);
  for (const sc of file.scenarios) sc.expect.max_words ??= file.default_max_words;
  const scenarios = file.scenarios.filter((s) => args.scenarios.length === 0 || args.scenarios.includes(s.id));
  if (scenarios.length === 0) exitWith("no scenario matches --scenario", 2);
  const checks = rubricChecks();
  if (checks.length === 0) exitWith("rubric.md has no checks; the format is '- R1a. text'", 1);

  const mode = args.dryRun ? "dry-run" : args.mode;
  const dir = join(resultsRoot, `${stampNow()}-${mode}`);
  writeJson(join(dir, "skills.json"), hashSkills());
  console.log(`Validated ${file.scenarios.length} scenarios and ${checks.length} rubric checks (${checks.filter((c) => c.auto).length} auto).`);

  if (args.dryRun) {
    for (const sc of scenarios) {
      writeText(join(dir, sc.id, "score-sheet.md"), scoreSheet(sc, mode, checks, null, null));
    }
    console.log(`Dry run: wrote score-sheet skeletons to ${dir}. No network calls were made.`);
    return;
  }

  const base = process.env.MULTICA_API ?? "http://localhost:8080";
  const token = process.env.MULTICA_TOKEN ?? "";
  const workspaceId = process.env.MULTICA_WORKSPACE_ID ?? "";
  const runtimeId = process.env.SECRETARY_EVAL_RUNTIME_ID ?? "";
  const secretaryId = process.env.SECRETARY_EVAL_SECRETARY_ID ?? "";
  if (!token || !workspaceId || !runtimeId || !secretaryId) {
    exitWith("Set MULTICA_TOKEN, MULTICA_WORKSPACE_ID, SECRETARY_EVAL_RUNTIME_ID and SECRETARY_EVAL_SECRETARY_ID.", 2);
  }
  const timeoutMs = Number(process.env.SECRETARY_EVAL_TIMEOUT_MS ?? 30 * 60 * 1000);
  const api = new Api(base, token, workspaceId);

  // Plumbing: pause the company first, so no run started by the seeding is executed.
  let priorSetting: string | null = null;
  if (args.mode === "plumbing") {
    const before = await api.call<CompanyState>("GET", "/api/company/company-state");
    priorSetting = before.setting;
    const paused = await api.call<CompanyState>("PUT", "/api/company/company-state", { state: "paused" });
    if (paused.state !== "paused") exitWith(`company is ${paused.state}, not paused; refusing to run plumbing mode`, 2);
  }

  const cleanup: Cleanup = { issues: [], squads: [], agents: [] };
  const summary: Record<string, unknown>[] = [];
  try {
    for (const sc of scenarios) {
      const scDir = join(dir, sc.id);
      writeText(join(scDir, "question.md"), sc.boss_question);
      writeJson(join(scDir, "scenario.json"), sc);
      if (args.mode === "plumbing") {
        const result = await runPlumbing(api, sc, runtimeId, secretaryId, cleanup, scDir);
        writeText(join(scDir, "score-sheet.md"), scoreSheet(sc, "plumbing", checks, null, result.checks));
        summary.push({ id: sc.id, wiring_pass: result.checks.every((c) => c.pass) });
      } else {
        const seeded = await seedCompany(api, sc, runtimeId, cleanup);
        const live = await runLive(api, sc, runtimeId, secretaryId, cleanup, scDir, timeoutMs, seeded);
        writeText(join(scDir, "score-sheet.md"), scoreSheet(sc, "live", checks, live, null));
        summary.push({ id: sc.id, words: live.assessment.split(/\s+/).length, created: live.created.length, challenge_comments: live.challengeComments });
      }
    }
  } finally {
    if (priorSetting !== null) {
      await api.call("PUT", "/api/company/company-state", { state: priorSetting });
    }
    if (!args.keep) {
      for (const id of cleanup.issues) await api.call("DELETE", `/api/issues/${id}`).catch(() => undefined);
      for (const id of cleanup.squads) await api.call("DELETE", `/api/squads/${id}`).catch(() => undefined);
      for (const id of cleanup.agents) await api.call("POST", `/api/agents/${id}/archive`).catch(() => undefined);
    }
  }

  writeJson(join(dir, "run.json"), { mode: args.mode, api: base, prior_setting: priorSetting, scenarios: summary, kept: args.keep });
  console.log(`Wrote outputs and score sheets to ${dir}.`);
}

main().catch((err: unknown) => exitWith(err instanceof Error ? err.message : String(err)));
