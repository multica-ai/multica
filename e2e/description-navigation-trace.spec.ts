import { expect, test, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

/**
 * MUL-7095 / PR #8092 Revision 3 — navigation performance recorder.
 *
 * Chromium only: the Long Tasks API (`PerformanceObserver` `longtask`) is
 * not implemented in WebKit, so `playwright.webkit.config.ts` does not match
 * this spec. It also asserts that support before measuring, so an engine
 * without it fails loudly instead of reporting an empty, clean-looking 0 ms.
 *
 * §0-A requirement: measurement must begin BEFORE issue navigation (at link
 * activation), not once the description host already exists. This spec
 * records, per navigation:
 *
 * - link click timestamp (`navClickT`): stamped at the target link's own
 *   click event (capture listener on the matching `a[href$="/issues/<id>"]`),
 *   not at a `page.evaluate` round-trip before `locator.click()`. Playwright's
 *   click waits for actionability first, so stamping pre-click would bill
 *   that wait (and its LongTasks) to click-to-commit.
 * - first committed detail frame for the TARGET issue (first rAF sample with
 *   `t >= clickT` whose `[data-tab-scroll-root]` exactly equals
 *   `main:<targetId>`, `firstDetailCommitT`). Matching the target id (not a
 *   `main:` prefix) and requiring `t >= clickT` keeps a stale root from a
 *   pre-click frame from passing as this navigation's commit.
 * - first frame containing the target issue's description surface, at or
 *   after this navigation's commit (`firstHostT`);
 * - first frame containing populated description content (`.ProseMirror`
 *   with non-empty text, at or after commit, `firstPopulatedT`);
 * - pre-click state of the target surface (`preClickTextLength`): the
 *   `.ProseMirror` text length at arm time, before the click. A non-zero
 *   value means the surface was already mounted and populated, which forces
 *   `firstPopulatedT` onto the commit frame and collapses
 *   `clickToPopulatedMs` onto `clickToCommitMs`. It is the auditable
 *   marker for that warm-retained condition, not a second timing signal.
 * - editor `isInitialized` at each sample;
 * - Long Tasks clipped to [click, first populated] (falls back to first
 *   commit only when the populated mark never fires — such a run fails the
 *   ordering assertions anyway): each overlapping entry contributes only
 *   its intersection (`overlapMs`), exported as both max (`maxOverlapMs`)
 *   and sum (`totalOverlapMs`). This keeps deferred parse/view work inside
 *   the measured window instead of hiding it past the route commit.
 *
 * No portable timing threshold is asserted: absolute numbers are
 * machine-specific. The spec writes the raw trace to a JSON attachment
 * (`navigation-trace`) and, when `NAV_TRACE_REPORT_PATH` is set, to that
 * file for ref-to-ref A/B runners. Ordering is the only in-spec invariant:
 * `clickT < firstDetailCommitT <= firstHostT <= firstPopulatedT`, plus one
 * populated initialized sample taken at or after the commit (an unqualified
 * `some(...)` would be satisfied by a retained surface's pre-click frames)
 * and both commit/populated timings.
 * A/B verdicts are relative guardrails applied outside this spec via
 * scripts/nav-trace-compare.mjs (base vs head vs revised, same fixture +
 * environment).
 *
 * Measurement vs acceptance (`NAV_TRACE_ACCEPTANCE`): the blank-free
 * requirement (zero `host && !populated` frames after commit) is a
 * head-acceptance assertion, not a measurement-validity check. The base
 * under comparison is known-bad exactly there — re-entry briefly blanks
 * the description — so gating sample usability on it would drop every
 * base sample and make the A/B unmeasurable. With
 * `NAV_TRACE_ACCEPTANCE=collect` (the runner uses this for the base side)
 * the trace is still fully recorded but the blank-free assertion is
 * skipped; the default (`accept`) enforces it for the head side. The
 * report records which mode produced it.
 *
 * Selectors are base/main/head compatible: the commit root
 * (`[data-tab-scroll-root]`) exists on all three refs, and the description
 * surface is found independently inside the target root: head's
 * `data-testid="issue-description"` div, falling back to the structurally
 * identical drop-zone container (`div.relative.mt-5.rounded-lg`) on
 * base/main. It is never resolved via `.ProseMirror.closest(...)`, so a
 * host-committed frame is recordable before any editor node exists.
 * Nothing here keys on
 * `data-testid="issue-description"`, so the same spec runs unchanged on
 * every ref.
 *
 * Cold compilation outliers are discarded separately: the first navigation
 * after load is a warm-up sample, excluded from the measured pass.
 */

type NavSample = {
  t: number;
  commitRoot: string | null;
  detailCommitted: boolean;
  hostPresent: boolean;
  proseMirrorPresent: boolean;
  populated: boolean;
  editorInitialized: boolean | null;
  descriptionTextLength: number;
};

type LongTaskEntry = {
  startTime: number;
  duration: number;
  name: string;
};

type OverlapTask = LongTaskEntry & {
  overlapStart: number;
  overlapEnd: number;
  overlapMs: number;
};

declare global {
  interface Window {
    __navInstalled: boolean;
    __navSamples: NavSample[];
    __navRecord: boolean;
    __navLongTasks: LongTaskEntry[];
    __navClickT: number | null;
    __navTargetId: string;
    __navArmed: boolean;
    __navFirstDetailCommitT: number | null;
    __navFirstHostT: number | null;
    __navFirstPopulatedT: number | null;
    __navPreClickTextLength: number | null;
    __navSamplerErrors: Array<{ t: number; message: string }>;
    __startNavRecording: (targetId: string) => void;
    __armNavRecording: (targetId: string) => void;
  }
}

/**
 * Base/main have no `data-testid="issue-description"` (added on the PR head
 * at issue-detail.tsx:3023). The container is still locatable on all refs:
 * it is the drop-zone div wrapping the description editor's `.ProseMirror`,
 * inside the detail scroll root. This mirrors that host div (same position
 * relative to `.ProseMirror`) instead of keying on the head-only testid.
 */
function descriptionSurface(page: Page, issueId?: string) {
  const root = issueId
    ? `[data-tab-scroll-root="main:${issueId}"]`
    : '[data-tab-scroll-root^="main:"]';
  // Host resolved independently of `.ProseMirror` (see the in-page sampler
  // below): head's testid div, else the structurally identical drop-zone
  // container on base/main. Never `.ProseMirror.closest(...)`.
  return page.locator(
    `${root} [data-testid="issue-description"], ${root} div.relative.mt-5.rounded-lg`,
  ).first();
}

const LONG_BODY = [
  "# Reentry A",
  "![Reentry image](/e2e-description.svg)",
  ...Array.from(
    { length: 100 },
    (_, i) =>
      `## Section ${i}\n\nParagraph ${i} of the cached description.\n\n- First item\n- Second item\n\n\`\`\`ts\nconst section = ${i};\n\`\`\``,
  ),
  "End of reentry A.",
].join("\n\n");

test.describe("MUL-7095 navigation performance (link-activation recorder)", () => {
  test.describe.configure({ timeout: 120000 });
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await page.route("**/e2e-description.svg", (route) =>
      route.fulfill({
        contentType: "image/svg+xml",
        headers: { "cache-control": "public, max-age=3600" },
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="320"><rect width="640" height="320" fill="teal"/></svg>',
      }),
    );
    await page.addInitScript(() => {
      // addInitScript re-runs on full document loads. The trace assumes
      // SPA client navigation (window state persists across open()/list
      // trips). The guard keeps a reload from double-registering the
      // observer (duplicate entries) or wiping an in-flight recording.
      if (window.__navInstalled) return;
      window.__navInstalled = true;
      window.__navSamples = [];
      window.__navLongTasks = [];
      window.__navRecord = false;
      window.__navClickT = null;
      window.__navTargetId = "";
      window.__navArmed = false;
      window.__navFirstDetailCommitT = null;
      window.__navFirstHostT = null;
      window.__navFirstPopulatedT = null;
      window.__navPreClickTextLength = null;
      window.__navSamplerErrors = [];
      // Started pre-click with buffered:true so tasks straddling navigation
      // are still observed; only the intersection with [click, first
      // populated] is counted at analysis time.
      const observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          window.__navLongTasks.push({
            startTime: entry.startTime,
            duration: entry.duration,
            name: entry.name,
          });
        }
      });
      observer.observe({ type: "longtask", buffered: true });
      const sample = () => {
        // Schedule the next frame FIRST: if the body below throws, the
        // loop survives and the failure is recorded instead of the
        // sampler going silently dark (MUL-7095 WebKit warm-run
        // diagnosis — the sampler fell silent ~70-90ms after click with
        // only 1-2 post-click frames).
        requestAnimationFrame(sample);
        if (!window.__navRecord) return;
        try {
          const now = performance.now();
          // Target-locked root: the exact `main:<targetId>` element, not
          // the first scroll root in the document. The list's `list` root
          // or another issue's `main:<other>` root must never satisfy
          // commit, host or populated — cross-issue isolation for all
          // three marks alike.
          const want = `main:${window.__navTargetId}`;
          const targetRoot =
            Array.from(
              document.querySelectorAll<HTMLElement>("[data-tab-scroll-root]"),
            ).find(
              (candidate) =>
                candidate.getAttribute("data-tab-scroll-root") === want,
            ) ?? null;
          const rootValue =
            targetRoot?.getAttribute("data-tab-scroll-root") ?? null;
          // Pre-click frames (clickT null, or now < clickT) never count,
          // even if a rAF fired between arming and the actual click.
          const clicked = window.__navClickT !== null;
          const visible = (targetRoot?.getClientRects().length ?? 0) > 0;
          const committed =
            clicked && now >= (window.__navClickT as number) && visible;
          // Host is resolved independently inside the TARGET root — head's
          // testid div, else the structurally identical drop-zone container
          // on base/main — never via `.ProseMirror.closest(...)`, so a
          // host-committed frame is recordable before any editor exists.
          const host =
            targetRoot?.querySelector<HTMLElement>(
              '[data-testid="issue-description"], div.relative.mt-5.rounded-lg',
            ) ?? null;
          const editor =
            targetRoot?.querySelector<HTMLElement>(".ProseMirror");
          const text = editor?.textContent ?? "";
          const initialized = (
            editor as
              | (HTMLElement & { editor?: { isInitialized: boolean } })
              | null
              | undefined
          )?.editor?.isInitialized;
          if (committed && window.__navFirstDetailCommitT === null) {
            window.__navFirstDetailCommitT = now;
          }
          // Host/populated lock to the target commit: only samples at or
          // after this navigation's commit may set them, so a stale
          // surface from another issue can never satisfy them first.
          if (committed && host && window.__navFirstHostT === null) {
            window.__navFirstHostT = now;
          }
          if (
            committed &&
            text.length > 0 &&
            window.__navFirstPopulatedT === null
          ) {
            window.__navFirstPopulatedT = now;
          }
          window.__navSamples.push({
            t: now,
            commitRoot: rootValue,
            detailCommitted: committed,
            hostPresent: !!host,
            proseMirrorPresent: !!editor,
            populated: text.length > 0,
            editorInitialized: initialized ?? null,
            descriptionTextLength: text.length,
          });
        } catch (err) {
          // Never swallow: record the sampler failure and keep looping.
          window.__navSamplerErrors.push({
            t: performance.now(),
            message: err instanceof Error ? err.message : String(err),
          });
        }
      };
      requestAnimationFrame(sample);
      // Arm the click stamp: the target link's own click event (capture)
      // records `performance.now()` in-page, so Playwright's pre-click
      // actionability wait and the evaluate→click round-trip are never
      // billed to click-to-commit or its LongTask window. Arming is
      // idempotent per navigation and scoped to the target href.
      window.__armNavRecording = (targetId: string) => {
        // Snapshot the target surface BEFORE the click. On a retained
        // surface the description is already mounted (hidden) here, so the
        // populated mark can only land on the commit frame; a non-zero
        // length makes that degenerate case auditable in the report.
        const want = `main:${targetId}`;
        const rootAtArm =
          Array.from(
            document.querySelectorAll<HTMLElement>("[data-tab-scroll-root]"),
          ).find(
            (candidate) =>
              candidate.getAttribute("data-tab-scroll-root") === want,
          ) ?? null;
        window.__navPreClickTextLength =
          rootAtArm?.querySelector<HTMLElement>(".ProseMirror")?.textContent
            ?.length ?? null;
        window.__navSamples = [];
        window.__navSamplerErrors = [];
        window.__navTargetId = targetId;
        window.__navClickT = null;
        window.__navArmed = true;
        window.__navFirstDetailCommitT = null;
        window.__navFirstHostT = null;
        window.__navFirstPopulatedT = null;
        window.__navRecord = true;
      };
      document.addEventListener(
        "click",
        (event) => {
          if (!window.__navArmed || window.__navClickT !== null) return;
          const anchor = (event.target as HTMLElement | null)?.closest?.(
            `a[href$="/issues/${window.__navTargetId}"]`,
          );
          if (anchor) window.__navClickT = performance.now();
        },
        true,
      );
      window.__startNavRecording = window.__armNavRecording;
    });
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  test("click-to-commit navigation trace with clipped Long Tasks", async ({
    page,
  }, testInfo) => {
    // Refuse to measure without the API this recorder is built on. The
    // Long Task list is empty on an engine that does not implement
    // `longtask`, and an empty list reads as a perfect 0 ms overlap — the
    // trace would pass as clean data instead of failing. Check the entry
    // type up front so the failure names the missing API.
    const longTaskSupported = await page.evaluate(() =>
      (PerformanceObserver.supportedEntryTypes ?? []).includes("longtask"),
    );
    expect(
      longTaskSupported,
      "This spec needs the Long Tasks API (PerformanceObserver 'longtask'), which WebKit does not implement — run it on Chromium.",
    ).toBe(true);

    const a = await api.createIssue(`E2E Nav A ${Date.now()}`, {
      description: LONG_BODY,
    });
    const slug = await loginAsDefault(page);
    const list = page.locator(`a[href="/${slug}/issues"]`).first();
    const openLink = (id: string) =>
      page.locator(`a[href$="/issues/${id}"]`).first();
    const surface = () => descriptionSurface(page, a.id);
    const editorReady = () =>
      page.waitForFunction(
        (targetId: string) => {
          const root = document.querySelector<HTMLElement>(
            `[data-tab-scroll-root="main:${targetId}"]`,
          );
          return (
            (
              root?.querySelector(".ProseMirror") as HTMLElement & {
                editor?: { isInitialized: boolean };
              }
            )?.editor?.isInitialized === true
          );
        },
        a.id,
        { timeout: 30000 },
      );

    // Warm-up navigation (cold compilation outlier — discarded from the
    // comparison, kept only so the measured pass is warm).
    await openLink(a.id).click();
    await expect(surface().locator(".ProseMirror")).toBeVisible({
      timeout: 30000,
    });
    const retainedToken = crypto.randomUUID();
    await surface().evaluate((node, token) => {
      node.setAttribute("data-nav-retained-token", token);
    }, retainedToken);
    await list.click();
    await expect(surface()).toBeHidden();

    // Measured navigation: arm the recorder, then click. The click stamp
    // lands at the target link's own click event (target-locked), so the
    // measured pass excludes Playwright's actionability wait.
    await page.evaluate((id: string) => window.__armNavRecording(id), a.id);
    await openLink(a.id).click();
    await expect(surface().locator(".ProseMirror")).toBeVisible({
      timeout: 30000,
    });
    const retainedSurface =
      (await surface().getAttribute("data-nav-retained-token")) === retainedToken;
    // Wait until the populated editor reports initialized so the trace
    // covers the full startup path, not just the first commit.
    await editorReady();
    // WebKit can resolve waitForFunction between paints. Let the recorder's
    // next rAF observe that ready frame before stopping it.
    await page.evaluate(
      () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
    );

    const trace = await page.evaluate(() => {
      window.__navRecord = false;
      const clickT = window.__navClickT;
      const targetId = window.__navTargetId;
      const firstCommit = window.__navFirstDetailCommitT;
      const firstPopulated = window.__navFirstPopulatedT;
      // MUL-7095 BLOCKER ②: LongTask window runs to firstPopulated, not
      // firstCommit. With `immediatelyRender: false` the heavy parse/view
      // work lands after the route's first commit — clipping at commit
      // would let a 300–500ms post-commit LongTask pass the gate.
      // `firstPopulated` falls back to `firstCommit` only when the
      // populated mark never fired (that run then fails the ordering
      // assertions below, so the fallback can never pass as data).
      const windowEnd = firstPopulated ?? firstCommit;
      // Clip each overlapping task to [click, windowEnd]: only the
      // intersection counts toward max/sum, so a task straddling the
      // window edge is never billed for work outside it.
      const overlapping: OverlapTask[] = [];
      if (clickT !== null && windowEnd !== null) {
        for (const task of window.__navLongTasks) {
          const overlapStart = Math.max(task.startTime, clickT);
          const overlapEnd = Math.min(task.startTime + task.duration, windowEnd);
          if (overlapEnd > overlapStart) {
            overlapping.push({
              ...task,
              overlapStart,
              overlapEnd,
              overlapMs: overlapEnd - overlapStart,
            });
          }
        }
      }
      return {
        targetId,
        clickT,
        firstDetailCommitT: firstCommit,
        firstHostT: window.__navFirstHostT,
        firstPopulatedT: firstPopulated,
        preClickTextLength: window.__navPreClickTextLength,
        samples: window.__navSamples,
        overlappingLongTasks: overlapping,
        maxOverlapMs: overlapping.reduce(
          (max, task) => Math.max(max, task.overlapMs),
          0,
        ),
        totalOverlapMs: overlapping.reduce(
          (sum, task) => sum + task.overlapMs,
          0,
        ),
        clickToCommitMs:
          clickT !== null && firstCommit !== null ? firstCommit - clickT : null,
        // Keep usable-description timing beside the first-commit metric.
        clickToPopulatedMs:
          clickT !== null && firstPopulated !== null ? firstPopulated - clickT : null,
        // Sampler-loop diagnosis: entries here mean the old code would
        // have gone dark at that frame (an empty array is the healthy
        // case and keeps the report schema backward compatible).
        samplerErrors: window.__navSamplerErrors ?? [],
      };
    });

    // MUL-7095: measurement vs acceptance split. The runner collects the
    // base side in `collect` mode, where the blank-free assertion below is
    // skipped (known-bad base blanks by design); the head side runs the
    // default `accept` mode. The mode is recorded on the report so the
    // runner can tell a skipped acceptance check from a real one.
    const acceptance =
      process.env.NAV_TRACE_ACCEPTANCE === "collect" ? "collect" : "accept";
    // Blank-free count is computed BEFORE the report is written (the write
    // below happens before the assertions, by design, so a failing run
    // still leaves its timing trace for the runner). In `accept` mode the
    // assertion further below enforces zero; in `collect` mode the count
    // is diagnosis only and never asserted.
    const blankFrameCount = trace.samples.filter(
      (s: NavSample) => s.detailCommitted && s.hostPresent && !s.populated,
    ).length;
    const report = {
      ...trace,
      status: "ok",
      fixture: "navigation-trace-v1",
      acceptance,
      blankFrames: blankFrameCount,
      retainedSurface,
    };
    await testInfo.attach("navigation-trace", {
      body: JSON.stringify(report, null, 2),
      contentType: "application/json",
    });
    const reportPath = process.env.NAV_TRACE_REPORT_PATH;
    if (reportPath) {
      const { writeFileSync } = await import("node:fs");
      writeFileSync(reportPath, JSON.stringify(report, null, 2));
    }

    // Strict ordering invariant: click < first commit <= host <= populated.
    // Null clickT fails here (listener never saw the target link's event),
    // so a stamp-less run can never pass as a measurement.
    expect(trace.clickT).not.toBeNull();
    expect(trace.firstDetailCommitT).not.toBeNull();
    expect(trace.clickToCommitMs).not.toBeNull();
    expect(trace.clickToCommitMs!).toBeGreaterThan(0);
    expect(trace.firstHostT).not.toBeNull();
    expect(trace.firstPopulatedT).not.toBeNull();
    expect(trace.firstHostT!).toBeGreaterThanOrEqual(
      trace.firstDetailCommitT!,
    );
    expect(trace.firstPopulatedT!).toBeGreaterThanOrEqual(
      trace.firstHostT!,
    );
    // At least one populated sample with an initialized editor, taken at or
    // after this navigation's commit. Pre-click samples are excluded on
    // purpose: a retained surface is already mounted and populated before
    // the click, so an unqualified `some(...)` would be satisfied by frames
    // that predate the navigation and prove nothing about it.
    expect(
      trace.samples.some(
        (s: NavSample) =>
          s.detailCommitted && s.populated && s.editorInitialized === true,
      ),
    ).toBe(true);
    // Blank-free requirement (head acceptance ONLY): once the target
    // route commits, no sampled frame may show the host without populated
    // content. Any such sample means the user saw an empty description
    // between commit and populate. Skipped in `collect` mode — the runner
    // collects the known-bad base that way, and a base that blanks by
    // design must still yield a timing sample, never an exclusion.
    // Sampler-infrastructure invariant (BOTH modes): a sampler throw —
    // even one the loop recovered from — may have skipped the frames where
    // a blank state would have been recorded. A trace with any sampler
    // error is not a valid measurement, base or head, collect or accept.
    expect(trace.samplerErrors).toEqual([]);
    if (acceptance === "accept") {
      expect(blankFrameCount).toBe(0);
      expect(retainedSurface).toBe(true);
    }
    // In `collect` mode the count stays on the report as diagnosis only
    // and is never asserted — a blanking base still yields its sample.
    // Populated timing stays defined for the relative guardrail report.
    expect(trace.clickToPopulatedMs).not.toBeNull();
    expect(trace.clickToPopulatedMs!).toBeGreaterThanOrEqual(
      trace.clickToCommitMs!,
    );

    // No absolute timing assertion here: A/B verdicts are relative
    // guardrails over the attached raw traces (base vs head vs revised,
    // same fixture + environment), not a portable millisecond threshold.
  });
});
