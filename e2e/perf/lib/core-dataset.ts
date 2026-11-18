/**
 * The "large workspace" dataset behind the core-path benchmark (MUL-9539).
 *
 * Sizes come from production aggregates (counts only, read replica,
 * 2026-11-18): the non-zero p99 per issue is 54 comments, 36 runs, 38 direct
 * sub-issues and 54 images; the p99 user has ~1,800 notifications, ~950 of
 * them unread; a p99 workspace has ~1,000 issues. `large` is the long issue
 * people actually report as slow (MUL-9538 and friends); `p99` is the 99th
 * percentile issue.
 *
 * Everything is synthetic and deterministic: the same DATASET_VERSION gives
 * the same text, structure and image bytes, and the manifest's digest proves
 * it, so two refs measured against one seeded database see identical data.
 * Rows go straight into the isolated local database (no agent ever runs);
 * images go through the real upload endpoint so their URLs, dimensions and
 * bytes are what the app normally serves.
 */
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import pg from "pg";
import { TestApiClient } from "../../fixtures";
import { mediaBytes } from "../fixtures/image-heavy-thread";

export const DATASET_VERSION = "core-2";
const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const PERF_EMAIL = "perf-core@multica.ai";
const PERF_WORKSPACE = { name: "Perf Core", slug: "perf-core" };

export interface IssueSpec {
  key: string;
  title: string;
  comments: number;
  images: number;
  runs: number;
  children: number;
  /** Where the notification points: a fraction of the way down the history. */
  target: number;
}

export interface SeededIssue extends IssueSpec {
  id: string;
  identifier: string;
  firstCommentId: string;
  lastCommentId: string;
  targetCommentId: string;
}

export interface Manifest {
  version: string;
  digest: string;
  database: string;
  workspace: { id: string; slug: string };
  token_email: string;
  issues: Record<string, SeededIssue>;
  /** The newest notifications, in Inbox order (newest first). */
  inbox: { key: string; notificationId: string }[];
  counts: Record<string, number>;
}

/** The two issues the "open a big issue" scenario measures. */
export const DETAIL_ISSUES = ["large", "p99"] as const;

function datasetSpec() {
  const issues: IssueSpec[] = [
    { key: "large", title: "Benchmark: long issue (300 comments, 80 images, 70 runs)", comments: 300, images: 80, runs: 70, children: 40, target: 0.8 },
    { key: "p99", title: "Benchmark: p99 issue (54 comments, 54 images, 36 runs)", comments: 54, images: 54, runs: 36, children: 38, target: 0.8 },
  ];
  for (let i = 0; i < 3; i++) {
    issues.push({ key: `medium-${i}`, title: `Benchmark: discussion ${i + 1} (120 comments)`, comments: 120, images: 10, runs: 15, children: 6, target: 1 });
  }
  const smallComments = [3, 8, 15, 25, 40];
  for (let i = 0; i < 45; i++) {
    issues.push({
      key: `small-${i}`,
      title: `Benchmark: inbox issue ${String(i + 1).padStart(2, "0")}`,
      comments: smallComments[i % smallComments.length]!,
      images: i % 3,
      runs: i % 4,
      children: i % 5 === 0 ? 3 : 0,
      target: i % 4 === 3 ? 0.5 : 1,
    });
  }
  // Inbox order, newest first: the big issues are spread through the first
  // twenty rows, the way they turn up among ordinary notifications.
  const order = [
    "small-0", "large", "small-1", "medium-0", "small-2", "small-3", "p99", "small-4", "medium-1", "small-5",
    "small-6", "small-7", "medium-2", "small-8", "small-9", "small-10", "small-11", "small-12", "small-13", "small-14",
    ...Array.from({ length: 30 }, (_, i) => `small-${15 + i}`),
  ];
  return { issues, order, backgroundIssues: 1000, backgroundNotifications: { unread: 900, read: 850 } };
}

/** Deterministic PRNG (mulberry32), so every seed of one version is identical. */
function rng(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const SENTENCES = [
  "The timeline now loads the latest page first and fetches older entries on demand.",
  "I reproduced this locally with the large fixture; the second click is where it stalls.",
  "Agreed, but we should keep the reading position stable when older comments arrive.",
  "The query plan switched to a sequential scan once the table passed a few thousand rows.",
  "Let's ship the narrow fix first and follow up with the refactor in a separate PR.",
  "Screenshots below are from the desktop build on macOS with the sidebar expanded.",
  "This only happens when the issue has more than a screenful of history.",
  "I checked the trace: most of the time goes to style recalculation after the list mounts.",
];

function commentBody(rand: () => number, n: number): string {
  const parts: string[] = [];
  const paragraphs = 1 + Math.floor(rand() * 4);
  for (let p = 0; p < paragraphs; p++) {
    const count = 2 + Math.floor(rand() * 4);
    parts.push(Array.from({ length: count }, () => SENTENCES[Math.floor(rand() * SENTENCES.length)]).join(" "));
  }
  const kind = n % 7;
  if (kind === 1) parts.push("```ts\nconst page = await api.listTimeline(issueId, { before: cursor, limit: 50 });\nfor (const entry of page.entries) render(entry);\nreturn page.next_cursor;\n```");
  if (kind === 3) parts.push("| Case | Before | After |\n| --- | --- | --- |\n| Open | 1840 ms | 420 ms |\n| Scroll | 12 stalls | 2 stalls |\n| Switch | 960 ms | 310 ms |");
  if (kind === 5) parts.push("- Keep the anchor comment in view\n- Load older pages on scroll\n- Leave the composer untouched\n  - including drafts");
  if (kind === 6) parts.push(`See \`useIssueTimeline\` and [the trace](https://example.com/trace/${n}).`);
  return parts.join("\n\n");
}

function description(title: string): string {
  return [
    `## Context\n\n${title}. ${SENTENCES.slice(0, 4).join(" ")}`,
    "## Acceptance\n\n- Opening the issue shows the first comments without a visible stall\n- Scrolling to the end stays smooth while images load\n- Toggling the sidebar does not re-render the history",
  ].join("\n\n");
}

/** The structure and text of the dataset; its hash identifies what was measured. */
export function datasetDigest(): string {
  const spec = datasetSpec();
  const rand = rng(9539);
  const bodies = spec.issues.map((issue) => Array.from({ length: issue.comments }, (_, i) => commentBody(rand, i)));
  return createHash("sha256")
    .update(JSON.stringify({ version: DATASET_VERSION, spec, bodies }))
    .update(mediaBytes().original)
    .digest("hex")
    .slice(0, 16);
}

export function manifestPath(): string {
  const db = new URL(process.env.DATABASE_URL!).pathname.slice(1);
  // Outside test-results/, which Playwright empties at the start of every run.
  return process.env.PERF_MANIFEST ?? join(tmpdir(), "multica-perf-core", `${db}.manifest.json`);
}

export async function perfApi(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(PERF_EMAIL, "Perf Reader");
  await api.ensureWorkspace(PERF_WORKSPACE.name, PERF_WORKSPACE.slug);
  await api.markUserOnboarded();
  return api;
}

/** Reuses the seeded dataset when the database still holds this version; seeds it otherwise. */
export async function ensureDataset(api: TestApiClient): Promise<Manifest> {
  const path = manifestPath();
  const digest = datasetDigest();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  try {
    if (existsSync(path)) {
      const manifest = JSON.parse(readFileSync(path, "utf8")) as Manifest;
      const large = manifest.issues.large;
      const alive = large && (await db.query("SELECT 1 FROM issue WHERE id=$1", [large.id])).rowCount === 1;
      if (manifest.version === DATASET_VERSION && manifest.digest === digest && alive) return manifest;
    }
    const manifest = await seed(api, db, digest);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, JSON.stringify(manifest, null, 2));
    return manifest;
  } finally {
    await db.end();
  }
}

async function upload(api: TestApiClient, slug: string, issueId: string, n: number): Promise<{ id: string; url: string }> {
  const form = new FormData();
  form.append("file", new Blob([new Uint8Array(mediaBytes().original)], { type: "image/png" }), `screenshot-${n}.png`);
  form.append("issue_id", issueId);
  const res = await fetch(`${API_BASE}/api/upload-file`, {
    method: "POST",
    headers: { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-Slug": slug },
    body: form,
  });
  if (!res.ok) throw new Error(`upload failed: ${res.status} ${await res.text()}`);
  const body = (await res.json()) as { id: string; markdown_url?: string; url: string };
  // Production persists MULTICA_PUBLIC_URL + /api/attachments/<id>/download, the
  // app's own origin; locally that origin is the web server's, so keep the path.
  const markdown = body.markdown_url || body.url;
  return { id: body.id, url: markdown.startsWith("http") ? new URL(markdown).pathname : markdown };
}

async function seed(api: TestApiClient, db: pg.Client, digest: string): Promise<Manifest> {
  const spec = datasetSpec();
  const rand = rng(9539);
  const workspace = (await api.getWorkspaces()).find((w) => w.slug === PERF_WORKSPACE.slug)!;
  const userId = (await db.query('SELECT id FROM "user" WHERE email=$1', [PERF_EMAIL])).rows[0].id as string;

  // Start from an empty workspace so a re-seed never stacks on an older version.
  await db.query("DELETE FROM inbox_item WHERE workspace_id=$1", [workspace.id]);
  await db.query("DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id=$1)", [workspace.id]);
  await db.query("DELETE FROM attachment WHERE workspace_id=$1", [workspace.id]);
  await db.query("DELETE FROM comment WHERE workspace_id=$1", [workspace.id]);
  await db.query("DELETE FROM issue WHERE workspace_id=$1", [workspace.id]);
  await db.query("DELETE FROM agent WHERE workspace_id=$1 AND name='Benchmark agent'", [workspace.id]);
  await db.query("DELETE FROM agent_runtime WHERE workspace_id=$1 AND name='Benchmark runtime'", [workspace.id]);

  const runtimeId = (await db.query(`INSERT INTO agent_runtime
    (workspace_id,name,runtime_mode,provider,status,device_info,metadata,owner_id)
    VALUES ($1,'Benchmark runtime','cloud','test','offline','Test','{}',$2) RETURNING id`, [workspace.id, userId])).rows[0].id as string;
  const agentId = (await db.query(`INSERT INTO agent
    (workspace_id,name,description,instructions,runtime_mode,runtime_config,runtime_id,visibility,permission_mode,max_concurrent_tasks,owner_id)
    VALUES ($1,'Benchmark agent','','','cloud','{}',$2,'workspace','private',1,$3) RETURNING id`, [workspace.id, runtimeId, userId])).rows[0].id as string;

  const statuses = ["backlog", "todo", "in_progress", "in_review", "done", "cancelled"] as const;
  const background = await api.seedTableIssues(Array.from({ length: spec.backgroundIssues }, (_, i) => ({
    title: `Background issue ${i + 1}: ${SENTENCES[i % SENTENCES.length]!.slice(0, 60)}`,
    status: statuses[i % statuses.length],
    priority: (["none", "low", "medium", "high", "urgent"] as const)[i % 5],
  })));

  const seeded: Record<string, SeededIssue> = {};
  let imageNo = 0;
  for (const issue of spec.issues) {
    const [row] = await api.seedTableIssues([{ title: issue.title, status: "in_progress", priority: "high" }]);
    const id = row!.id;
    await db.query("UPDATE issue SET description=$2, created_at=now()-interval '30 days' WHERE id=$1", [id, description(issue.title)]);
    if (issue.children > 0) {
      await api.seedTableIssues(Array.from({ length: issue.children }, (_, i) => ({
        title: `${issue.title.split(" (")[0]} · sub-issue ${i + 1}`,
        status: statuses[i % statuses.length],
        parentIssueId: id,
      })));
    }

    // Threads of 1–6 comments; every other comment by the agent.
    const commentIds: string[] = [];
    let root: string | null = null;
    let threadLeft = 0;
    const bodies = Array.from({ length: issue.comments }, (_, i) => commentBody(rand, i));
    const imageSlots = new Map<number, number>();
    for (let k = 0; k < issue.images; k++) {
      const at = Math.floor(((k % Math.max(1, Math.ceil(issue.images / 2))) * issue.comments) / Math.ceil(issue.images / 2));
      imageSlots.set(at, (imageSlots.get(at) ?? 0) + 1);
    }
    const startOffset = issue.comments + 10;
    for (let i = 0; i < issue.comments; i++) {
      if (threadLeft === 0) {
        root = null;
        threadLeft = 1 + (i % 6);
      }
      const byAgent = i % 2 === 1;
      const commentId = (await db.query(`
        INSERT INTO comment (workspace_id,issue_id,author_type,author_id,content,parent_id,created_at,updated_at)
        VALUES ($1,$2,$3,$4,$5,$6,now()-($7::int*interval '1 minute'),now()-($7::int*interval '1 minute')) RETURNING id`,
      [workspace.id, id, byAgent ? "agent" : "member", byAgent ? agentId : userId, bodies[i], root, startOffset - i])).rows[0].id as string;
      const images = imageSlots.get(i) ?? 0;
      if (images > 0) {
        const lines: string[] = [];
        for (let k = 0; k < images; k++) {
          const file = await upload(api, workspace.slug, id, imageNo++);
          await db.query("UPDATE attachment SET comment_id=$2 WHERE id=$1", [file.id, commentId]);
          lines.push(`![screenshot-${imageNo}.png](${file.url})`);
        }
        await db.query("UPDATE comment SET content=content || $2 WHERE id=$1", [commentId, `\n\n${lines.join("\n\n")}`]);
      }
      commentIds.push(commentId);
      if (!root) root = commentId;
      threadLeft--;
    }

    for (let r = 0; r < issue.runs; r++) {
      await db.query(`INSERT INTO agent_task_queue
        (agent_id,runtime_id,issue_id,status,result,handoff_note,created_at,started_at,completed_at)
        VALUES ($1,$2,$3,'completed',$4,$5,now()-($6::int*interval '1 minute'),now()-($6::int*interval '1 minute'),now()-($6::int*interval '1 minute')+interval '3 minutes')`,
      [agentId, runtimeId, id, JSON.stringify({ comment: "Run summary", output: "provider output line\n".repeat(800) }), "Handoff context. ".repeat(40), startOffset - Math.floor((r * issue.comments) / Math.max(1, issue.runs))]);
    }

    const identifier = (await db.query("SELECT w.issue_prefix || '-' || i.number AS identifier FROM issue i JOIN workspace w ON w.id=i.workspace_id WHERE i.id=$1", [id])).rows[0].identifier as string;
    seeded[issue.key] = {
      ...issue,
      id,
      identifier,
      firstCommentId: commentIds[0]!,
      lastCommentId: commentIds.at(-1)!,
      targetCommentId: commentIds[Math.min(commentIds.length - 1, Math.floor((commentIds.length - 1) * issue.target))]!,
    };
  }

  // Background notifications: older than every measured row, on the background issues.
  const { unread, read } = spec.backgroundNotifications;
  await db.query(`
    INSERT INTO inbox_item (workspace_id,recipient_type,recipient_id,actor_type,actor_id,type,issue_id,title,read,created_at)
    SELECT $1,'member',$2,'agent',$3,'status_changed',issue_ids[1 + (g % array_length(issue_ids,1))],'Background notification',g >= $4,now()-interval '2 days'-(g*interval '1 minute')
    FROM generate_series(0,$5::int-1) AS g, (SELECT $6::uuid[] AS issue_ids) ids`,
  [workspace.id, userId, agentId, unread, unread + read, background.map((b) => b.id)]);

  const inbox: Manifest["inbox"] = [];
  for (const [position, key] of spec.order.entries()) {
    const issue = seeded[key]!;
    const notificationId = (await db.query(`
      INSERT INTO inbox_item (workspace_id,recipient_type,recipient_id,actor_type,actor_id,type,issue_id,title,details,created_at)
      VALUES ($1,'member',$2,'agent',$3,'new_comment',$4,$5,$6::jsonb,now()-($7::int*interval '1 minute')) RETURNING id`,
    [workspace.id, userId, agentId, issue.id, issue.title, JSON.stringify({ comment_id: issue.targetCommentId }), position + 1])).rows[0].id as string;
    inbox.push({ key, notificationId });
  }

  const counts = (await db.query(`SELECT
      (SELECT count(*) FROM issue WHERE workspace_id=$1)::int AS issues,
      (SELECT count(*) FROM comment WHERE workspace_id=$1)::int AS comments,
      (SELECT count(*) FROM attachment WHERE workspace_id=$1)::int AS images,
      (SELECT count(*) FROM agent_task_queue WHERE agent_id=$2)::int AS runs,
      (SELECT count(*) FROM inbox_item WHERE workspace_id=$1)::int AS notifications,
      (SELECT count(*) FROM inbox_item WHERE workspace_id=$1 AND NOT read)::int AS unread`, [workspace.id, agentId])).rows[0] as Record<string, number>;

  return {
    version: DATASET_VERSION,
    digest,
    database: new URL(process.env.DATABASE_URL!).pathname.slice(1),
    workspace: { id: workspace.id, slug: workspace.slug },
    token_email: PERF_EMAIL,
    issues: seeded,
    inbox,
    counts,
  };
}

/** Puts the measured notifications back to unread, so every sample starts from the same Inbox. */
export async function resetInbox(manifest: Manifest): Promise<void> {
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  try {
    await db.query("UPDATE inbox_item SET read=false, archived=false WHERE id = ANY($1::uuid[])", [manifest.inbox.map((n) => n.notificationId)]);
  } finally {
    await db.end();
  }
}
