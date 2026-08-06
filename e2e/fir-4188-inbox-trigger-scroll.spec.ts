import { test, expect, type Locator, type Page } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";
import { loginAsDefault, createTestApi } from "./helpers";

/**
 * FIR-4188 — opening an Inbox row must land on the message that triggered it
 * and STAY there, on all three row kinds: issue notification, Direct Message,
 * agent chat.
 *
 * Each surface runs a ONE-SHOT landing scroll — `useHighlightCommentScroll` for
 * issues, the `scrollTop = scrollHeight` layout effect for channels/DMs,
 * `useStickyBottom` for agent chats — and then never re-aims. Anything that
 * changes the height of content ABOVE the landing position after that moment
 * pushes the user's reading position away. This spec pins that contract: land,
 * let the slowest known async render finish, assert the target has not moved.
 *
 * The async render it exercises is bare issue-identifier resolution
 * (`useResolveIssueIdentifier`), held at the network layer so each surface is
 * forced to land BEFORE resolution and re-render after it. Nothing else is
 * stubbed; every position is real layout, measured in a real browser.
 *
 * WHAT THIS SPEC DOES NOT DO — read before treating it as proof of the PR #2836
 * fix. It passes both with and without that fix, because the height change the
 * fix removes is not measurable here. Measured on this suite's own scenario:
 *   - issue comments (ReadonlyContent → IssueChip, `inline`): 60 chips resolved,
 *     pane scrollHeight 1942 → 1942. Zero movement.
 *   - agent chat (Markdown → IssueMentionCard, `inline-flex`): 30 chips
 *     resolved, scrollHeight 1049 → 1057 — 8px total, which useStickyBottom
 *     absorbs; the target moved 0.5px.
 * So this is a landing-stability guard, not a reproduction of FIR-4188. The
 * cause of the reported jumping is still unidentified; see the issue.
 *
 * Content is seeded via SQL on purpose: the comment API rewrites a bare
 * identifier into an explicit `mention://issue/<uuid>` link on write, which
 * bypasses the autolink path this spec drives. Stored content with bare
 * identifiers is what that renderer exists for.
 */

const DATABASE_URL = process.env.DATABASE_URL!;

// Enough repeats to overflow every pane, so there is real scrolling to lose,
// and enough resolutions that any per-chip height change would compound past
// the tolerance below rather than hiding in rounding.
const IDENTIFIER_REPEATS = 30;

// Sub-pixel layout jitter (fractional line-heights, scrollbar rounding) is not
// a regression. Anything a user would perceive as a jump is far above this.
const MAX_DRIFT_PX = 2;

// Each case logs in, seeds three surfaces and waits out two real network
// round-trips, which does not fit the 60s project default.
test.describe.configure({ mode: "serial", retries: 2, timeout: 120000 });

let api: TestApiClient;
let pgc: pg.Client;

test.beforeEach(async () => {
  api = await createTestApi();
  await api.resetInboxItems();
  pgc = new pg.Client(DATABASE_URL);
  await pgc.connect();
});

test.afterEach(async () => {
  await pgc.end();
  await api.cleanup();
});

function identifierBlock(identifier: string): string {
  return Array.from({ length: IDENTIFIER_REPEATS }, () => identifier).join("\n\n");
}

/**
 * Hold every identifier-resolution request until the returned `release` is
 * called. Only requests for this test's identifier are held — the inbox itself
 * searches, and stalling that would change what we are measuring.
 */
async function holdIdentifierResolution(page: Page, identifier: string) {
  let released = false;
  const waiters: Array<() => void> = [];

  await page.route("**/api/issues/search**", async (route) => {
    const q = new URL(route.request().url()).searchParams.get("q");
    if (q !== identifier) return route.fallback();
    if (!released) {
      await new Promise<void>((resolve) => waiters.push(resolve));
    }
    await route.fallback();
  });

  return () => {
    released = true;
    for (const w of waiters.splice(0)) w();
  };
}

/** Vertical position of `target` relative to its own scroll pane. */
async function offsetInPane(target: Locator, pane: Locator): Promise<number> {
  const [t, p] = await Promise.all([target.boundingBox(), pane.boundingBox()]);
  if (!t || !p) throw new Error("target or pane not laid out");
  return t.y - p.y;
}

/** Wait until the landing scroll has stopped moving `target`. */
async function settle(target: Locator, pane: Locator): Promise<number> {
  let last = Number.NaN;
  for (let i = 0; i < 40; i++) {
    const now = await offsetInPane(target, pane);
    if (Math.abs(now - last) < 0.5) return now;
    last = now;
    await target.page().waitForTimeout(100);
  }
  return last;
}

/**
 * The scroll pane that owns `target` — found in the browser so the assertion
 * follows the real overflow container rather than a guessed selector, which
 * differs across the three surfaces.
 */
async function scrollPaneOf(page: Page, target: Locator): Promise<Locator> {
  const id = await target.evaluate((el) => {
    let node: HTMLElement | null = el.parentElement;
    while (node) {
      const overflowY = getComputedStyle(node).overflowY;
      if (
        (overflowY === "auto" || overflowY === "scroll") &&
        node.scrollHeight > node.clientHeight
      ) {
        node.dataset.e2eScrollPane = "1";
        return "1";
      }
      node = node.parentElement;
    }
    return null;
  });
  if (!id) {
    const chain = await target.evaluate((el) => {
      const rows: string[] = [];
      let n: HTMLElement | null = el.parentElement;
      while (n) {
        rows.push(`${n.tagName}.${n.className?.toString().slice(0,30)} oy=${getComputedStyle(n).overflowY} sh=${n.scrollHeight} ch=${n.clientHeight}`);
        n = n.parentElement;
      }
      return rows;
    });
    console.log("ANCESTORS:\n" + chain.join("\n"));
    throw new Error("no scrolling ancestor found for target");
  }
  return page.locator("[data-e2e-scroll-pane='1']");
}

/**
 * Wait until every seeded identifier has turned into a link inside `pane`.
 *
 * The signal is the anchor COUNT, not the anchor text: an unresolved identifier
 * is plain text, while a resolved one is an anchor in every version — inline
 * text today, an IssueChip before the fix (whose visible text is the issue
 * title, not the identifier). Counting keeps the wait valid when this spec is
 * run against unfixed code to confirm it fails.
 */
async function waitForIdentifiersResolved(
  pane: Locator,
  anchorsBefore: number,
): Promise<void> {
  await expect
    .poll(() => pane.locator("a").count(), {
      timeout: 20000,
      message: "seeded issue identifiers never resolved into links",
    })
    .toBeGreaterThanOrEqual(anchorsBefore + IDENTIFIER_REPEATS);
  // Let the resulting reflow finish before re-measuring.
  await pane.page().waitForTimeout(500);
}

async function seedIssueComment(
  issueId: string,
  wsId: string,
  userId: string,
  content: string,
): Promise<string> {
  const res = await pgc.query<{ id: string }>(
    `INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
     VALUES ($1, $2, 'member', $3, $4, 'comment')
     RETURNING id`,
    [issueId, wsId, userId, content],
  );
  return res.rows[0]!.id;
}

test("issue notification stays on the triggering comment after identifiers resolve", async ({
  page,
}) => {
  const slug = await loginAsDefault(page, { workspaceReadyTimeout: 45000 });
  const tag = Math.random().toString(36).slice(2, 8);
  const wsId = api.getWorkspaceId()!;
  const userId = api.getUserId()!;

  const referenced = await api.createIssue(`FIR-4188 referenced ${tag}`);
  const identifier = referenced.identifier as string;
  const target = await api.createIssue(`FIR-4188 issue target ${tag}`);

  await seedIssueComment(target.id, wsId, userId, identifierBlock(identifier));
  const triggerText = `ISSUE-TRIGGER-${tag}`;
  const triggerId = await seedIssueComment(target.id, wsId, userId, triggerText);

  await api.insertInboxItem({
    type: "new_comment",
    route: "inbox",
    title: `FIR-4188 issue row ${tag}`,
    issueId: target.id,
    details: { comment_id: triggerId },
  });

  const release = await holdIdentifierResolution(page, identifier);

  await page.goto(`/${slug}/inbox`);
  await page.getByText(`FIR-4188 issue row ${tag}`).first().click();

  const trigger = page.locator(`#comment-${triggerId}`);
  await expect(trigger).toBeVisible({ timeout: 20000 });
  const pane = await scrollPaneOf(page, trigger);

  const landed = await settle(trigger, pane);
  const anchorsBefore = await pane.locator("a").count();

  release();
  await waitForIdentifiersResolved(pane, anchorsBefore);

  const after = await offsetInPane(trigger, pane);
  expect(
    Math.abs(after - landed),
    `triggering comment moved ${(after - landed).toFixed(1)}px after issue identifiers resolved`,
  ).toBeLessThanOrEqual(MAX_DRIFT_PX);
});

test("direct message stays on its newest message after identifiers resolve", async ({
  page,
}) => {
  const slug = await loginAsDefault(page, { workspaceReadyTimeout: 45000 });
  const tag = Math.random().toString(36).slice(2, 8);
  const wsId = api.getWorkspaceId()!;
  const userId = api.getUserId()!;

  const referenced = await api.createIssue(`FIR-4188 dm referenced ${tag}`);
  const identifier = referenced.identifier as string;

  const other = await api.loginSecondaryUser(
    `e2e-fir4188-${tag}@multica.ai`,
    `FIR-4188 Partner ${tag}`,
  );
  const dm = await api.createDirectMessage(other.userId);

  await seedIssueComment(dm.id, wsId, userId, identifierBlock(identifier));
  const triggerText = `DM-TRIGGER-${tag}`;
  const triggerId = await seedIssueComment(dm.id, wsId, other.userId, triggerText);

  await api.insertInboxItem({
    type: "new_comment",
    route: "inbox",
    title: `FIR-4188 dm row ${tag}`,
    issueId: dm.id,
    actorType: "member",
    actorId: other.userId,
    details: { comment_id: triggerId },
  });

  const release = await holdIdentifierResolution(page, identifier);

  await page.goto(`/${slug}/inbox`);
  await page.getByText(`FIR-4188 Partner ${tag}`).first().click();

  const trigger = page.locator(`[data-message-id="${triggerId}"]`);
  await expect(trigger).toBeVisible({ timeout: 20000 });
  const pane = await scrollPaneOf(page, trigger);

  const landed = await settle(trigger, pane);
  const anchorsBefore = await pane.locator("a").count();

  release();
  await waitForIdentifiersResolved(pane, anchorsBefore);

  const after = await offsetInPane(trigger, pane);
  expect(
    Math.abs(after - landed),
    `newest direct message moved ${(after - landed).toFixed(1)}px after issue identifiers resolved`,
  ).toBeLessThanOrEqual(MAX_DRIFT_PX);
});

test("agent chat stays on its newest message after identifiers resolve", async ({
  page,
}) => {
  const slug = await loginAsDefault(page, { workspaceReadyTimeout: 45000 });
  const tag = Math.random().toString(36).slice(2, 8);
  const wsId = api.getWorkspaceId()!;
  const userId = api.getUserId()!;

  const referenced = await api.createIssue(`FIR-4188 chat referenced ${tag}`);
  const identifier = referenced.identifier as string;

  const runtime = await pgc.query<{ id: string }>(
    `INSERT INTO agent_runtime
       (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
     VALUES ($1, NULL, $2, 'cloud', 'e2e_fir4188', 'online', 'E2E', '{}'::jsonb, now())
     RETURNING id`,
    [wsId, `FIR-4188 runtime ${tag}`],
  );
  const agent = await pgc.query<{ id: string }>(
    `INSERT INTO agent
       (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
     VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
     RETURNING id`,
    [wsId, `FIR-4188 Agent ${tag}`, runtime.rows[0]!.id, userId],
  );
  const sessionTitle = `FIR-4188 chat ${tag}`;
  const session = await pgc.query<{ id: string }>(
    `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
     VALUES ($1, $2, $3, $4, 'active')
     RETURNING id`,
    [wsId, agent.rows[0]!.id, userId, sessionTitle],
  );
  const sessionId = session.rows[0]!.id;

  await pgc.query(
    `INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', $2)`,
    [sessionId, identifierBlock(identifier)],
  );
  const triggerText = `CHAT-TRIGGER-${tag}`;
  await pgc.query(
    `INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'assistant', $2)`,
    [sessionId, triggerText],
  );

  const release = await holdIdentifierResolution(page, identifier);

  await page.goto(`/${slug}/inbox`);
  await page.getByText(sessionTitle).first().click();

  const trigger = page.getByText(triggerText).first();
  await expect(trigger).toBeVisible({ timeout: 20000 });
  const pane = await scrollPaneOf(page, trigger);

  const landed = await settle(trigger, pane);
  const anchorsBefore = await pane.locator("a").count();

  release();
  await waitForIdentifiersResolved(pane, anchorsBefore);

  const after = await offsetInPane(trigger, pane);
  expect(
    Math.abs(after - landed),
    `newest agent chat message moved ${(after - landed).toFixed(1)}px after issue identifiers resolved`,
  ).toBeLessThanOrEqual(MAX_DRIFT_PX);
});
