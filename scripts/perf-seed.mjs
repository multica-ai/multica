#!/usr/bin/env node
// Seed a large, reproducible workspace for the issue-detail and Inbox
// performance scenarios (MUL-9539), then write a manifest the specs read:
//
//   make up C=api ARGS="--ephemeral"
//   make env-exec ARGS="-- node scripts/perf-seed.mjs --tier m --out perf-work/manifest-m.json"
//
// Tier sizes come from production distributions (aggregate counts only):
//   m — nonzero p99: 1,000 issues per workspace, 5,000 notifications per user.
//   l — the largest observed: 60,000 issues, 130,000 notifications, plus an
//       extreme issue with 2,000 comments and 300 images.
// Every tier also carries three "big" issues (300 comments, 80 images, 70 runs,
// 40 sub-issues) and five p99 issues; those are what the scenarios open.
//
// Bulk rows go straight into the environment's database; images go through
// the real upload endpoint so the API serves real bytes with real dimensions.
// Content is generated from a fixed seed: two runs of one tier produce the same
// text (the manifest's content digest says so), with different row ids.

import { createHash, randomUUID } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { deflateSync } from "node:zlib";

import pg from "pg";

const DATASET_VERSION = 1;

const BIG = { comments: 300, images: 80, runs: 70, children: 40 };
const P99 = { comments: 54, images: 54, runs: 36, children: 38 };
const XL = { comments: 2000, images: 300, runs: 500, children: 200 };

const TIERS = {
  m: { issues: 1_000, notifications: 5_000, big: 3, p99: 5, xl: 0 },
  l: { issues: 60_000, notifications: 130_000, big: 3, p99: 5, xl: 1 },
};

// The Inbox scenario's newest 50 rows, top to bottom. `big`/`p99`/`xl` take
// the next issue of that shape; `mid` has 10–30 comments; `small` has 0–3.
const INBOX_PATTERN = ["big", "mid", "small", "p99", "mid", "small", "small", "mid", "p99", "small"];
const INBOX_ROWS = 50;

const USAGE = `Usage: node scripts/perf-seed.mjs --tier m|l --out <manifest.json> [--seed N]

Run through \`make env-exec ARGS="-- node scripts/perf-seed.mjs --tier m --out perf-work/manifest-m.json"\`.`;

function fail(message) {
  console.error(`[perf-seed] ${message}`);
  process.exit(1);
}

function parseCli(argv) {
  const options = { tier: null, out: null, seed: 9539 };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--tier") options.tier = argv[++i];
    else if (arg === "--out") options.out = argv[++i];
    else if (arg === "--seed") options.seed = Number(argv[++i]);
    else if (arg === "--help" || arg === "-h") {
      console.log(USAGE);
      process.exit(0);
    } else fail(`unknown argument ${arg}\n\n${USAGE}`);
  }
  if (!TIERS[options.tier]) fail(`--tier must be one of ${Object.keys(TIERS).join(", ")}\n\n${USAGE}`);
  if (!options.out) fail(`--out is required\n\n${USAGE}`);
  if (!Number.isInteger(options.seed)) fail("--seed must be an integer");
  return options;
}

function loadProfile(env) {
  if (!env.MULTICA_DEV_PROFILE) fail("MULTICA_DEV_PROFILE is not set; run this through `make env-exec`.");
  if (!env.DATABASE_URL) fail("DATABASE_URL is not set; run this through `make env-exec`.");
  const home = env.MULTICA_DEV_PROFILES_HOME || join(homedir(), ".multica", "profiles");
  const file = join(home, env.MULTICA_DEV_PROFILE, "config.json");
  let config;
  try {
    config = JSON.parse(readFileSync(file, "utf8"));
  } catch (error) {
    fail(`could not read ${file}: ${error.message}\nRun \`make up\` first; it signs the profile in.`);
  }
  if (!config.token) fail(`${file} has no token; re-run \`make up\`.`);
  return {
    apiUrl: env.PORT ? `http://localhost:${env.PORT}` : config.server_url,
    token: config.token,
    databaseUrl: env.DATABASE_URL,
  };
}

function client({ apiUrl, token }) {
  let workspaceId = null;
  const headers = () => ({
    Authorization: `Bearer ${token}`,
    ...(workspaceId ? { "X-Workspace-ID": workspaceId } : {}),
  });
  async function request(method, path, body) {
    const response = await fetch(`${apiUrl}${path}`, {
      method,
      headers: { ...headers(), "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    if (!response.ok) throw new Error(`${method} ${path} → ${response.status} ${text.slice(0, 300)}`);
    return text ? JSON.parse(text) : null;
  }
  async function upload(issueId, filename, bytes) {
    const form = new FormData();
    form.append("file", new Blob([bytes], { type: "image/png" }), filename);
    form.append("issue_id", issueId);
    const response = await fetch(`${apiUrl}/api/upload-file`, { method: "POST", headers: headers(), body: form });
    const text = await response.text();
    if (!response.ok) throw new Error(`upload ${filename} → ${response.status} ${text.slice(0, 300)}`);
    return JSON.parse(text);
  }
  return { request, upload, setWorkspace: (id) => (workspaceId = id) };
}

// ------------------------------------------------------------------ random ---

function mulberry32(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function makeRandom(seed) {
  const next = mulberry32(seed);
  const int = (min, max) => min + Math.floor(next() * (max - min + 1));
  const pick = (items) => items[Math.floor(next() * items.length)];
  return { next, int, pick, chance: (p) => next() < p };
}

// ----------------------------------------------------------------- content ---

const SENTENCES = [
  "The timeline now loads the newest page first and fetches older entries on demand.",
  "I reproduced the stall on a workspace with a few hundred comments and several large screenshots.",
  "Opening the issue from the Inbox scrolls to the comment, but the main thread is busy for a while after that.",
  "The sidebar animation re-measures every visible row, which is where most of the frame time goes.",
  "This keeps the reading position when new replies arrive while the user is scrolled up.",
  "We should verify the change on both the desktop app and the web build before merging.",
  "The query plan switched to an index scan once the partial index was in place.",
  "Image placeholders reserve their final size so the thread no longer jumps while they load.",
  "I left the existing behaviour alone for archived notifications; only the live list changes.",
  "The regression test pins the exact failure: a streamed message replacing the summary node.",
  "Memory stayed flat across twenty switches once the detail panel stopped retaining old timelines.",
  "Next step is to rerun the benchmark against main and attach the comparison to the PR.",
  "The agent picked up the follow-up and opened a draft with the migration and the handler change.",
  "Most of the cost is markdown parsing for comments that are never on screen.",
  "Deferring the detail render keeps the row highlight responsive, but the swap itself is still heavy.",
];

const CODE_BLOCKS = [
  "```ts\nconst page = await api.listTimeline(issueId, { limit: 50, before: cursor });\nsetEntries((prev) => mergeByCreatedAt(page.entries, prev));\nreturn page.has_more_before;\n```",
  "```go\nrows, err := q.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{\n\tIssueID: issue.ID,\n\tLimit:   pageSize + 1,\n})\nif err != nil {\n\treturn nil, fmt.Errorf(\"list comments: %w\", err)\n}\n```",
  "```sql\nSELECT count(*) FILTER (WHERE NOT read) AS unread\nFROM inbox_item\nWHERE workspace_id = $1 AND recipient_id = $2 AND NOT archived;\n```",
  "```diff\n-  const items = useMemo(() => buildTimeline(entries), [entries]);\n+  const items = useMemo(() => buildTimeline(entries, { collapseResolved }), [entries, collapseResolved]);\n```",
];

const TABLE =
  "| Scenario | Before | After | Change |\n| --- | ---: | ---: | ---: |\n| Open long issue | 2,140 ms | 610 ms | -71% |\n| Inbox → comment | 4,820 ms | 930 ms | -81% |\n| Sidebar toggle | 380 ms | 42 ms | -89% |";

const LOG_LINE =
  "2026-11-17T09:12:44.118Z WARN api/inbox unread_count workspace=perf recipient=member elapsed_ms=4821 rows_scanned=129786 plan=SeqScan reason=\"missing partial index on (recipient_id) WHERE NOT archived\"";

function paragraphs(rng, min, max) {
  return Array.from({ length: rng.int(min, max) }, () =>
    Array.from({ length: rng.int(1, 4) }, () => rng.pick(SENTENCES)).join(" "),
  );
}

function memberComment(rng) {
  const parts = paragraphs(rng, 1, 3);
  if (rng.chance(0.3)) parts.push(Array.from({ length: rng.int(2, 5) }, () => `- ${rng.pick(SENTENCES)}`).join("\n"));
  if (rng.chance(0.15)) parts.push(rng.pick(CODE_BLOCKS));
  if (rng.chance(0.05)) parts.push("```\n" + LOG_LINE + "\n```");
  return parts.join("\n\n");
}

function agentReply(rng, n) {
  const parts = [`## Run ${n} summary`, ...paragraphs(rng, 2, 4)];
  parts.push(Array.from({ length: rng.int(3, 6) }, (_, i) => `- [${i % 3 === 0 ? " " : "x"}] ${rng.pick(SENTENCES)}`).join("\n"));
  if (rng.chance(0.5)) parts.push(rng.pick(CODE_BLOCKS));
  if (rng.chance(0.3)) parts.push(TABLE);
  return parts.join("\n\n");
}

function description(rng, title) {
  return [`${title}.`, ...paragraphs(rng, 2, 3), Array.from({ length: 4 }, () => `- ${rng.pick(SENTENCES)}`).join("\n")].join("\n\n");
}

function runOutput(rng) {
  const lines = [];
  const target = rng.int(4, 32) * 1024;
  let size = 0;
  while (size < target) {
    const line = `[tool] ${rng.pick(SENTENCES)} ${rng.pick(["ok", "ok", "retry", "skipped"])}`;
    lines.push(line);
    size += line.length + 1;
  }
  return lines.join("\n");
}

// ------------------------------------------------------------------ images ---

function crc32(bytes) {
  let crc = ~0;
  for (const byte of bytes) {
    crc ^= byte;
    for (let k = 0; k < 8; k += 1) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
  }
  return ~crc >>> 0;
}

function chunk(type, data) {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([length, body, crc]);
}

/**
 * A 2880×1800 screenshot-like PNG: flat panels, text-like rows, and one
 * photo-like noisy panel so the file lands near a real capture (~1 MB)
 * instead of compressing to a few kilobytes.
 */
function screenshotPNG(variant, width = 2880, height = 1800) {
  const stride = width * 3 + 1;
  const raw = Buffer.alloc(stride * height);
  const rng = makeRandom(1000 + variant);
  const panel = [rng.int(200, 250), rng.int(200, 250), rng.int(200, 250)];
  const photo = { x: 1500, y: 300, w: 720, h: 440 };
  for (let y = 0; y < height; y += 1) {
    const row = y * stride;
    const textRow = y % 28 > 8 && y % 28 < 20 && (y >> 5) % 3 !== 0;
    for (let x = 0; x < width; x += 1) {
      const offset = row + 1 + x * 3;
      if (x >= photo.x && x < photo.x + photo.w && y >= photo.y && y < photo.y + photo.h) {
        raw[offset] = rng.int(0, 255);
        raw[offset + 1] = rng.int(0, 255);
        raw[offset + 2] = rng.int(0, 255);
        continue;
      }
      const sidebar = x < 520;
      const ink = textRow && x % 640 > 80 && x % 640 < 560 && ((x * 7 + y * 13 + variant) % 11 < 4);
      const base = sidebar ? [panel[0] - 40, panel[1] - 40, panel[2] - 30] : panel;
      raw[offset] = ink ? 40 : base[0];
      raw[offset + 1] = ink ? 44 : base[1];
      raw[offset + 2] = ink ? 52 : base[2];
    }
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 2;
  return { header, idat: deflateSync(raw, { level: 6 }) };
}

/** Distinct bytes per image (so nothing is served from cache twice), same pixels per variant. */
function imageBytes(base, n) {
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", base.header),
    chunk("tEXt", Buffer.from(`Comment\0perf-seed image ${n}`, "latin1")),
    chunk("IDAT", base.idat),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

// -------------------------------------------------------------------- seed ---

async function insertRows(db, table, columns, rows) {
  for (let start = 0; start < rows.length; start += 1000) {
    const slice = rows.slice(start, start + 1000);
    const values = [];
    const tuples = slice.map((row, r) => `(${columns.map((_, c) => {
      values.push(row[c]);
      return `$${r * columns.length + c + 1}`;
    }).join(",")})`);
    await db.query(`INSERT INTO ${table} (${columns.join(",")}) VALUES ${tuples.join(",")}`, values);
  }
}

async function main() {
  const options = parseCli(process.argv.slice(2));
  const tier = TIERS[options.tier];
  const profile = loadProfile(process.env);
  const api = client(profile);
  const rng = makeRandom(options.seed);
  const digest = createHash("sha256");
  const started = Date.now();
  const log = (message) => console.error(`[perf-seed] ${((Date.now() - started) / 1000).toFixed(1)}s ${message}`);

  const me = await api.request("GET", "/api/me");
  const suffix = randomUUID().slice(0, 8);
  const workspace = await api.request("POST", "/api/workspaces", {
    name: `Perf benchmark (${options.tier})`,
    slug: `perf-${options.tier}-${suffix}`,
  });
  api.setWorkspace(workspace.id);
  const prefix = workspace.issue_prefix || "PERF";
  log(`workspace ${workspace.slug}`);

  const db = new pg.Client(profile.databaseUrl);
  await db.connect();
  try {
    // A first-run questionnaire over the page would be measured with it.
    await db.query(
      `UPDATE "user" SET onboarded_at = COALESCE(onboarded_at, now()),
         onboarding_questionnaire = onboarding_questionnaire || '{"source":["friends_colleagues"],"source_other":null,"source_skipped":false}'::jsonb
       WHERE id = $1`,
      [me.id],
    );
    // People: production p99 is three members per workspace.
    const members = [me.id];
    for (let i = 1; i <= 2; i += 1) {
      const id = randomUUID();
      await db.query(`INSERT INTO "user" (id, name, email) VALUES ($1, $2, $3)`, [id, `Perf Member ${i}`, `perf-${suffix}-${i}@example.test`]);
      await db.query(`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, [workspace.id, id]);
      members.push(id);
    }
    const runtimeId = (await db.query(
      `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
       VALUES ($1, 'Perf runtime', 'cloud', 'test', 'offline', 'Perf', '{}', $2) RETURNING id`,
      [workspace.id, me.id],
    )).rows[0].id;
    const agentId = (await db.query(
      `INSERT INTO agent (workspace_id, name, description, instructions, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
       VALUES ($1, 'Perf Agent', '', '', 'cloud', '{}', $2, 'workspace', 'private', 1, $3) RETURNING id`,
      [workspace.id, runtimeId, me.id],
    )).rows[0].id;

    let number = 0;
    const now = Date.now();
    const at = (msAgo) => new Date(now - msAgo).toISOString();
    const STATUSES = ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"];
    const PRIORITIES = ["urgent", "high", "medium", "low", "none"];

    // Filler issues, each with one comment so comment notifications have a target.
    const fillerStart = number + 1;
    await db.query(
      `INSERT INTO issue (workspace_id, title, description, status, priority, assignee_type, assignee_id, creator_type, creator_id, number, position, created_at, updated_at, last_activity_at)
       SELECT $1,
              'Workspace issue ' || n || ': ' || (ARRAY['Inbox unread count','Timeline paging','Search timeout','Sidebar toggle','Image lazy load','Run transcript','Chat history','Agent visibility'])[1 + n % 8],
              CASE WHEN n % 3 = 0 THEN 'Background for workspace issue ' || n || '. ' || repeat('Details about the change, the risk and how it was verified. ', 1 + n % 6) END,
              (ARRAY['backlog','todo','in_progress','in_review','done','done','done','cancelled'])[1 + n % 8],
              (ARRAY['urgent','high','medium','low','none'])[1 + n % 5],
              CASE WHEN n % 4 = 0 THEN 'agent' WHEN n % 4 = 1 THEN 'member' END,
              CASE WHEN n % 4 = 0 THEN $2::uuid WHEN n % 4 = 1 THEN $3::uuid END,
              'member', $3, n, n,
              now() - (n || ' minutes')::interval, now() - (n || ' minutes')::interval, now() - (n || ' minutes')::interval
       FROM generate_series($4::int, $5::int) n`,
      [workspace.id, agentId, me.id, fillerStart, fillerStart + tier.issues - 1],
    );
    number += tier.issues;
    await db.query(
      `INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, created_at, updated_at)
       SELECT workspace_id, id, 'member', $2, 'Follow-up on ' || title || ': verified locally, numbers attached in the PR.', created_at + interval '5 minutes', created_at + interval '5 minutes'
       FROM issue WHERE workspace_id = $1`,
      [workspace.id, members[1]],
    );
    log(`${tier.issues} filler issues`);

    const imageBases = [0, 1, 2, 3].map((variant) => screenshotPNG(variant));
    let imageCounter = 0;

    async function seedIssue(kind, shape, index) {
      const title = `${kind.toUpperCase()} ${index + 1}: long-running discussion with ${shape.comments} comments and ${shape.images} images`;
      number += 1;
      const issueNumber = number;
      const createdMsAgo = 30 * 86_400_000;
      const issueId = (await db.query(
        `INSERT INTO issue (workspace_id, title, description, status, priority, assignee_type, assignee_id, creator_type, creator_id, number, position, created_at, updated_at, last_activity_at)
         VALUES ($1, $2, $3, 'in_progress', 'high', 'agent', $4, 'member', $5, $6::int, $6::int, $7, now(), now()) RETURNING id`,
        [workspace.id, title, description(rng, title), agentId, me.id, issueNumber, at(createdMsAgo)],
      )).rows[0].id;
      digest.update(title);

      // Images upload first: their markdown URLs go into the comments below.
      const images = [];
      for (let i = 0; i < shape.images; i += 1) {
        imageCounter += 1;
        const uploaded = await api.upload(issueId, `screenshot-${imageCounter}.png`, imageBytes(imageBases[imageCounter % 4], imageCounter));
        images.push(uploaded);
      }

      // Timeline: runs are spread evenly; every run is triggered by the member
      // comment before it and answered by one agent comment.
      const step = createdMsAgo / (shape.comments + 2);
      const comments = [];
      const runs = [];
      const runEvery = shape.runs > 0 ? Math.max(1, Math.floor(shape.comments / shape.runs)) : Infinity;
      let roots = [];
      let lastMember = null;
      for (let i = 0; i < shape.comments; i += 1) {
        const createdAt = at(createdMsAgo - step * (i + 1));
        const isRunReply = runs.length < shape.runs && i % runEvery === runEvery - 1;
        const id = randomUUID();
        let content;
        let authorType = "member";
        let authorId = members[i % members.length];
        let parentId = null;
        let sourceTaskId = null;
        if (isRunReply) {
          const runId = randomUUID();
          const status = runs.length % 14 === 13 ? "failed" : runs.length % 30 === 29 ? "cancelled" : "completed";
          runs.push([runId, agentId, runtimeId, issueId, status, JSON.stringify({ comment: "Run finished", output: runOutput(rng) }),
            lastMember, at(createdMsAgo - step * (i + 1) + 120_000), createdAt, createdAt]);
          content = agentReply(rng, runs.length);
          authorType = "agent";
          authorId = agentId;
          sourceTaskId = runId;
        } else {
          content = memberComment(rng);
          if (roots.length > 0 && rng.chance(0.3)) parentId = rng.pick(roots.slice(-8));
        }
        // Digest the generated text only: ids and upload URLs differ per run.
        digest.update(`${authorType}:${parentId ? "reply" : "root"}:${content}`);
        comments.push({ id, content, authorType, authorId, parentId, sourceTaskId, createdAt });
        if (!parentId) roots.push(id);
        if (authorType === "member" && !parentId) lastMember = id;
      }
      // One or two images per comment, on the comments nearest the even spread.
      const imageOwners = [];
      if (images.length > 0) {
        const carriers = Math.max(1, Math.ceil(images.length / (images.length > shape.comments ? 2 : 1.3)));
        const every = Math.max(1, Math.floor(comments.length / carriers));
        let next = 0;
        for (let c = 0; c < comments.length && next < images.length; c += every) {
          const files = images.slice(next, next + (next + 1 < images.length && (c / every) % 3 === 1 ? 2 : 1));
          next += files.length;
          digest.update(`images:${c}:${files.length}`);
          comments[c].content += "\n\n" + files.map((f) => `![${f.filename}](${f.markdown_url ?? f.url})`).join("\n\n");
          for (const f of files) imageOwners.push([f.id, comments[c].id]);
        }
        for (; next < images.length; next += 1) {
          const last = comments[comments.length - 1];
          digest.update(`images:${comments.length - 1}:1`);
          last.content += `\n\n![${images[next].filename}](${images[next].markdown_url ?? images[next].url})`;
          imageOwners.push([images[next].id, last.id]);
        }
      }

      // Comments first: a run's insert trigger resolves its trigger comment's thread.
      await insertRows(db, "comment",
        ["id", "workspace_id", "issue_id", "author_type", "author_id", "content", "parent_id", "source_task_id", "created_at", "updated_at"],
        comments.map((c) => [c.id, workspace.id, issueId, c.authorType, c.authorId, c.content, c.parentId, c.sourceTaskId, c.createdAt, c.createdAt]),
      );
      await insertRows(db, "agent_task_queue",
        ["id", "agent_id", "runtime_id", "issue_id", "status", "result", "trigger_comment_id", "started_at", "completed_at", "created_at"],
        runs,
      );
      for (const [attachmentId, commentId] of imageOwners) {
        await db.query(`UPDATE attachment SET comment_id = $2 WHERE id = $1`, [attachmentId, commentId]);
      }

      // Status history, so the timeline mixes activity rows with comments.
      const activities = Array.from({ length: Math.min(40, Math.ceil(shape.comments / 8)) }, (_, i) => {
        const from = STATUSES[i % 4];
        const to = STATUSES[(i + 1) % 4];
        return [workspace.id, issueId, "member", members[i % members.length], "status_changed", JSON.stringify({ from, to }), at(createdMsAgo - step * (i * 8 + 4))];
      });
      await insertRows(db, "activity_log", ["workspace_id", "issue_id", "actor_type", "actor_id", "action", "details", "created_at"], activities);

      const children = [];
      for (let i = 0; i < shape.children; i += 1) {
        number += 1;
        children.push([workspace.id, `${title.split(":")[0]} · sub-issue ${i + 1}`, STATUSES[i % STATUSES.length], PRIORITIES[i % PRIORITIES.length],
          "agent", agentId, "member", me.id, issueId, number, number, at(createdMsAgo - i * 60_000)]);
      }
      await insertRows(db, "issue",
        ["workspace_id", "title", "status", "priority", "assignee_type", "assignee_id", "creator_type", "creator_id", "parent_issue_id", "number", "position", "created_at"],
        children,
      );

      return {
        id: issueId,
        identifier: `${prefix}-${issueNumber}`,
        title,
        kind,
        shape,
        first_comment_id: comments[0]?.id ?? null,
        comment_ids: comments.map((c) => c.id),
      };
    }

    const scenario = { big: [], p99: [], xl: [], mid: [], small: [] };
    for (let i = 0; i < tier.big; i += 1) scenario.big.push(await seedIssue("big", BIG, i));
    log(`${tier.big} big issues`);
    for (let i = 0; i < tier.p99; i += 1) scenario.p99.push(await seedIssue("p99", P99, i));
    log(`${tier.p99} p99 issues`);
    for (let i = 0; i < tier.xl; i += 1) scenario.xl.push(await seedIssue("xl", XL, i));
    if (tier.xl) log(`${tier.xl} xl issues`);
    const midCount = INBOX_PATTERN.filter((k) => k === "mid").length * (INBOX_ROWS / INBOX_PATTERN.length);
    const smallCount = INBOX_PATTERN.filter((k) => k === "small").length * (INBOX_ROWS / INBOX_PATTERN.length);
    for (let i = 0; i < midCount; i += 1) {
      scenario.mid.push(await seedIssue("mid", { comments: rng.int(10, 30), images: rng.int(0, 5), runs: rng.int(1, 5), children: rng.int(0, 5) }, i));
    }
    for (let i = 0; i < smallCount; i += 1) {
      scenario.small.push(await seedIssue("small", { comments: rng.int(0, 3), images: 0, runs: 0, children: 0 }, i));
    }
    log(`${midCount} mid and ${smallCount} small issues`);
    await db.query(`UPDATE workspace SET issue_counter = $2 WHERE id = $1`, [workspace.id, number]);

    // Older notifications across the workspace, half of them unread.
    const fillerNotifications = tier.notifications - INBOX_ROWS;
    await db.query(
      `CREATE TEMP TABLE perf_targets AS
       SELECT (row_number() OVER (ORDER BY i.number) - 1)::int AS k, i.id, i.title, c.id AS comment_id
       FROM issue i JOIN comment c ON c.issue_id = i.id
       WHERE i.workspace_id = $1 AND i.number BETWEEN $2 AND $3`,
      [workspace.id, fillerStart, fillerStart + tier.issues - 1],
    );
    await db.query("ALTER TABLE perf_targets ADD PRIMARY KEY (k)");
    await db.query(
      `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, body, read, archived, created_at, actor_type, actor_id, details)
       SELECT $1, 'member', $2,
              (ARRAY['new_comment','status_changed','mentioned','issue_assigned','priority_changed'])[1 + n % 5],
              'info', t.id, t.title,
              CASE WHEN n % 5 IN (0, 2) THEN 'Follow-up: verified locally, numbers attached in the PR.' END,
              n % 2 = 0, n % 10 = 0,
              now() - interval '2 hours' - (n || ' minutes')::interval,
              'member', $3::uuid,
              CASE WHEN n % 5 IN (0, 2) THEN jsonb_build_object('comment_id', t.comment_id)
                   WHEN n % 5 = 1 THEN '{"from":"todo","to":"in_progress"}'::jsonb
                   WHEN n % 5 = 4 THEN '{"from":"low","to":"high"}'::jsonb
                   ELSE '{}'::jsonb END
       FROM generate_series(1, $4::int) n JOIN perf_targets t ON t.k = n % $5::int`,
      [workspace.id, me.id, members[1], fillerNotifications, tier.issues],
    );
    await db.query("DROP TABLE perf_targets");
    log(`${fillerNotifications} older notifications`);

    // The Inbox scenario's rows: newest first, unread, one per issue.
    const take = { big: 0, p99: 0, xl: 0, mid: 0, small: 0 };
    const inbox = [];
    for (let row = 0; row < INBOX_ROWS; row += 1) {
      let kind = INBOX_PATTERN[row % INBOX_PATTERN.length];
      if (kind === "big" && scenario.xl.length > 0 && row === INBOX_PATTERN.length * 2) kind = "xl";
      const pool = scenario[kind];
      const issue = pool[take[kind]++ % pool.length];
      const comment = issue.comment_ids.length > 0 && row % 3 !== 2
        ? issue.comment_ids[Math.floor(issue.comment_ids.length * (0.6 + 0.35 * ((row * 7) % 10) / 10))]
        : null;
      const id = (await db.query(
        `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, body, read, archived, created_at, actor_type, actor_id, details)
         VALUES ($1, 'member', $2, $3, 'info', $4, $5, $6, false, false, now() - ($7 || ' seconds')::interval, 'member', $8, $9) RETURNING id`,
        [workspace.id, me.id, comment ? "new_comment" : "status_changed", issue.id, issue.title,
          comment ? "Replied in the thread with the benchmark numbers." : null, row * 30, members[2],
          JSON.stringify(comment ? { comment_id: comment } : { from: "in_progress", to: "in_review" })],
      )).rows[0].id;
      inbox.push({ inbox_item_id: id, issue_id: issue.id, title: issue.title, kind, target_comment_id: comment, comments: issue.shape.comments });
    }
    log(`${INBOX_ROWS} Inbox scenario rows`);
    await db.query("ANALYZE issue; ANALYZE comment; ANALYZE inbox_item; ANALYZE attachment; ANALYZE agent_task_queue;");

    const strip = ({ comment_ids, ...issue }) => issue;
    const manifest = {
      dataset: "perf-large-workspace",
      dataset_version: DATASET_VERSION,
      tier: options.tier,
      seed: options.seed,
      content_sha256: digest.digest("hex"),
      created_at: new Date().toISOString(),
      api_url: profile.apiUrl,
      workspace: { id: workspace.id, slug: workspace.slug },
      user: { id: me.id, email: me.email },
      counts: {
        workspace_issues: number,
        notifications: tier.notifications,
        images: imageCounter,
      },
      issues: {
        big: scenario.big.map(strip),
        p99: scenario.p99.map(strip),
        xl: scenario.xl.map(strip),
      },
      inbox,
    };
    const out = resolve(options.out);
    mkdirSync(dirname(out), { recursive: true });
    writeFileSync(out, JSON.stringify(manifest, null, 2));
    log(`manifest → ${out}`);
  } finally {
    await db.end();
  }
}

main().catch((error) => fail(error.stack || error.message));
