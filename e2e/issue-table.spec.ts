import {
  expect,
  test,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { createTestApi, loginAsDefault, reloadAppPage } from "./helpers";
import type { TestApiClient, TestTableIssueSeed } from "./fixtures";

type TableRequestBody = {
  group?: { kind?: string };
  group_key?: string | null;
  parent_id?: string | null;
  page?: { limit?: number; cursor?: string | null };
};

type TableGroupsResponse = {
  total: number;
  groups: Array<{
    key: string;
    count: number;
    value: { kind: string; status?: string };
  }>;
};

type TableRowsResponse = {
  total: number;
  next_cursor: string | null;
  rows: Array<{
    issue: { id: string; title: string; status: string };
    direct_child_count: number;
  }>;
};

function apiPath(request: Request) {
  return new URL(request.url()).pathname;
}

function tableBody(request: Request): TableRequestBody {
  return (request.postDataJSON() ?? {}) as TableRequestBody;
}

async function switchToTable(page: Page) {
  const currentView = page.getByRole("button", { name: "Board", exact: true });
  await expect(currentView).toBeVisible();
  await currentView.click();
  const tableOption = page.getByRole("menuitemradio", {
    name: "Table",
    exact: true,
  });
  await tableOption.click();
  await expect(
    page.getByRole("button", { name: "Table", exact: true }),
  ).toBeVisible();
  await expect(tableOption).toBeHidden();
}

async function groupByStatus(page: Page) {
  await page.getByRole("button", { name: "Group", exact: true }).click();
  const statusOption = page.getByRole("menuitemradio", {
    name: "Status",
    exact: true,
  });
  await statusOption.click();
  await expect(
    page.getByRole("button", { name: "Group: Status", exact: true }),
  ).toBeVisible();
  await expect(statusOption).toBeHidden();
}

test.describe("Issue Table server grouping", () => {
  test.describe.configure({ timeout: 120000 });

  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  test("groups 1,001 issues exactly without materializing the full result", async ({
    page,
  }) => {
    const run = Date.now();
    const seeds: TestTableIssueSeed[] = Array.from({ length: 1001 }, (_, index) => ({
      title: `E2E Table Large ${run} ${index.toString().padStart(4, "0")}`,
      status: index < 501 ? "backlog" : index < 801 ? "todo" : "done",
      position: index + 1,
    }));
    await api.seedTableIssues(seeds);
    await reloadAppPage(page);

    const tableRowRequests: TableRequestBody[] = [];
    const legacyMembershipRequests: string[] = [];
    let lastObservedRequestAt = Date.now();
    const collectRequest = (request: Request) => {
      const path = apiPath(request);
      if (path === "/api/issues/table/rows") {
        lastObservedRequestAt = Date.now();
        tableRowRequests.push(tableBody(request));
      }
      if (path === "/api/issues" || path === "/api/issues/query") {
        lastObservedRequestAt = Date.now();
        legacyMembershipRequests.push(request.url());
      }
    };

    page.on("request", collectRequest);
    // The page initially mounts Board, whose seven status buckets may still
    // be finishing when the header becomes interactive. Observe that startup
    // traffic, wait for it to settle, then clear it while keeping the listener
    // installed so any request caused by the Table switch is captured.
    await expect
      .poll(() => Date.now() - lastObservedRequestAt, {
        timeout: 5000,
        intervals: [100, 200, 300],
      })
      .toBeGreaterThan(750);
    tableRowRequests.length = 0;
    legacyMembershipRequests.length = 0;
    lastObservedRequestAt = Date.now();
    await switchToTable(page);
    const groupsResponsePromise = page.waitForResponse(
      (response) =>
        apiPath(response.request()) === "/api/issues/table/groups" &&
        response.status() === 200,
    );
    await groupByStatus(page);

    const groupsResponse = await groupsResponsePromise;
    const groups = (await groupsResponse.json()) as TableGroupsResponse;
    expect(groups.total).toBe(1001);
    expect(
      Object.fromEntries(groups.groups.map((group) => [group.key, group.count])),
    ).toEqual({
      "status:backlog": 501,
      "status:todo": 300,
      "status:done": 200,
    });

    const backlogGroup = page
      .getByRole("row")
      .filter({ hasText: "Backlog" })
      .first();
    await expect(backlogGroup).toContainText("501");
    await expect(page.getByText(/Loaded \d+ of 1001/)).toBeVisible();
    await expect(
      page.getByText(/Grouping and hierarchy are paused/),
    ).toHaveCount(0);

    await expect
      .poll(() => Date.now() - lastObservedRequestAt, {
        timeout: 5000,
        intervals: [100, 200, 300],
      })
      .toBeGreaterThan(750);
    page.off("request", collectRequest);
    expect(legacyMembershipRequests).toEqual([]);
    expect(tableRowRequests.length).toBeGreaterThan(0);
    expect(tableRowRequests.length).toBeLessThan(8);
    expect(
      tableRowRequests.every(
        (body) =>
          (body.page?.limit ?? 0) <= 50 &&
          (body.page?.cursor === null || body.page?.cursor === undefined),
      ),
    ).toBe(true);
  });

  test("keeps same-group children nested and cross-group children at the group root", async ({
    page,
  }) => {
    const run = Date.now();
    const parentTitle = `E2E Hierarchy Parent ${run}`;
    const sameGroupTitle = `E2E Hierarchy Same Group ${run}`;
    const crossGroupTitle = `E2E Hierarchy Cross Group ${run}`;
    const [parent] = await api.seedTableIssues([
      { title: parentTitle, status: "todo", position: 1 },
    ]);
    if (!parent) throw new Error("Hierarchy parent fixture was not created");
    await api.seedTableIssues([
      {
        title: sameGroupTitle,
        status: "todo",
        parentIssueId: parent.id,
        position: 1,
      },
      {
        title: crossGroupTitle,
        status: "done",
        parentIssueId: parent.id,
        position: 2,
      },
    ]);
    await reloadAppPage(page);
    await switchToTable(page);

    const todoRootPromise = page.waitForResponse((response) => {
      if (apiPath(response.request()) !== "/api/issues/table/rows") return false;
      const body = tableBody(response.request());
      return (
        body.group_key === "status:todo" &&
        body.parent_id === null &&
        response.status() === 200
      );
    });
    const todoChildrenPromise = page.waitForResponse((response) => {
      if (apiPath(response.request()) !== "/api/issues/table/rows") return false;
      const body = tableBody(response.request());
      return body.parent_id === parent.id && response.status() === 200;
    });
    const doneRootPromise = page.waitForResponse((response) => {
      if (apiPath(response.request()) !== "/api/issues/table/rows") return false;
      const body = tableBody(response.request());
      return (
        body.group_key === "status:done" &&
        body.parent_id === null &&
        response.status() === 200
      );
    });

    await groupByStatus(page);
    const [todoRootResponse, todoChildrenResponse, doneRootResponse] =
      await Promise.all([
        todoRootPromise,
        todoChildrenPromise,
        doneRootPromise,
      ]);
    const todoRoot = (await todoRootResponse.json()) as TableRowsResponse;
    const todoChildren =
      (await todoChildrenResponse.json()) as TableRowsResponse;
    const doneRoot = (await doneRootResponse.json()) as TableRowsResponse;

    expect(todoRoot.total).toBe(3);
    expect(todoRoot.rows).toEqual([
      expect.objectContaining({
        issue: expect.objectContaining({ id: parent.id, title: parentTitle }),
        direct_child_count: 1,
      }),
    ]);
    expect(todoChildren.rows.map((row) => row.issue.title)).toEqual([
      sameGroupTitle,
    ]);
    expect(doneRoot.rows.map((row) => row.issue.title)).toEqual([
      crossGroupTitle,
    ]);

    const sameGroupRow = page
      .getByRole("row")
      .filter({ hasText: sameGroupTitle });
    const crossGroupRow = page
      .getByRole("row")
      .filter({ hasText: crossGroupTitle });
    await expect(sameGroupRow).toBeVisible();
    await expect(crossGroupRow).toBeVisible();
    await expect(
      sameGroupRow.getByRole("button", { name: sameGroupTitle }).locator(".."),
    ).toHaveCSS("padding-left", "18px");
    await expect(
      crossGroupRow.getByRole("button", { name: crossGroupTitle }).locator(".."),
    ).toHaveCSS("padding-left", "0px");
  });

  test("drops stale branch cursors after a realtime sort-boundary update", async ({
    page,
  }) => {
    const run = Date.now();
    const seeds = await api.seedTableIssues(
      Array.from({ length: 60 }, (_, index) => ({
        title: `E2E Table Cursor ${run} ${index.toString().padStart(2, "0")}`,
        status: "todo" as const,
        position: index + 1,
      })),
    );
    const moved = seeds.at(-1);
    if (!moved) throw new Error("Cursor fixture was not created");
    await reloadAppPage(page);

    const firstHeadPromise = page.waitForResponse((response) => {
      if (apiPath(response.request()) !== "/api/issues/table/rows") return false;
      const body = tableBody(response.request());
      return (
        body.group?.kind === "none" &&
        (body.page?.cursor === null || body.page?.cursor === undefined) &&
        response.status() === 200
      );
    });
    await switchToTable(page);
    const firstHead = (await (await firstHeadPromise).json()) as TableRowsResponse;
    const staleCursor = firstHead.next_cursor;
    expect(staleCursor).toBeTruthy();

    const firstTailPromise = page.waitForResponse((response) => {
      if (apiPath(response.request()) !== "/api/issues/table/rows") return false;
      const body = tableBody(response.request());
      return body.page?.cursor === staleCursor && response.status() === 200;
    });
    const tableScroller = page.locator("table").locator("..");
    await tableScroller.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await firstTailPromise;
    await expect(page.getByText("Loaded 60 of 60", { exact: true })).toBeVisible();

    const postUpdateResponses: Array<{
      body: TableRequestBody;
      payload: TableRowsResponse;
    }> = [];
    const collectResponse = async (response: Response) => {
      if (
        apiPath(response.request()) !== "/api/issues/table/rows" ||
        response.status() !== 200
      ) {
        return;
      }
      postUpdateResponses.push({
        body: tableBody(response.request()),
        payload: (await response.json()) as TableRowsResponse,
      });
    };
    page.on("response", collectResponse);
    await api.updateIssue(moved.id, { position: 0 });

    await expect
      .poll(
        () =>
          postUpdateResponses.find(
            ({ body }) =>
              body.page?.cursor === null || body.page?.cursor === undefined,
          )?.payload.next_cursor,
        { timeout: 10000 },
      )
      .not.toBeUndefined();
    const freshHead = postUpdateResponses.find(
      ({ body }) =>
        body.page?.cursor === null || body.page?.cursor === undefined,
    )?.payload;
    const freshCursor = freshHead?.next_cursor;
    expect(freshCursor).toBeTruthy();
    expect(freshCursor).not.toBe(staleCursor);

    await tableScroller.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect
      .poll(
        () =>
          postUpdateResponses.find(
            ({ body }) => body.page?.cursor === freshCursor,
          )?.payload,
        { timeout: 10000 },
      )
      .not.toBeUndefined();
    const freshTail = postUpdateResponses.find(
      ({ body }) => body.page?.cursor === freshCursor,
    )?.payload;
    const refreshedIds = [
      ...(freshHead?.rows ?? []),
      ...(freshTail?.rows ?? []),
    ].map((row) => row.issue.id);
    expect(new Set(refreshedIds).size).toBe(60);
    expect(refreshedIds).toContain(moved.id);
    await expect(page.getByText("Loaded 60 of 60", { exact: true })).toBeVisible();
    page.off("response", collectResponse);
  });

  test("shows a branch-level retry and never falls back to client grouping", async ({
    page,
  }) => {
    const title = `E2E Table Branch Retry ${Date.now()}`;
    await api.seedTableIssues([{ title, status: "todo", position: 1 }]);
    await reloadAppPage(page);

    let allowRows = false;
    let forcedFailures = 0;
    await page.route("**/api/issues/table/rows", async (route) => {
      const body = tableBody(route.request());
      if (
        body.group?.kind === "status" &&
        body.group_key === "status:todo" &&
        !allowRows
      ) {
        forcedFailures += 1;
        await route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({
            error: "forced_e2e_failure",
            message: "Forced Table branch failure",
          }),
        });
        return;
      }
      await route.continue();
    });

    await switchToTable(page);
    await groupByStatus(page);
    const retry = page.getByRole("button", {
      name: "Loading more failed — Retry",
      exact: true,
    });
    await expect(retry).toBeVisible({ timeout: 20000 });
    await expect(page.getByText(title)).toHaveCount(0);
    expect(forcedFailures).toBeGreaterThan(0);

    allowRows = true;
    await retry.click();
    await expect(page.getByText(title)).toBeVisible({ timeout: 10000 });
    await expect(retry).toHaveCount(0);
  });
});
