import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Issues", () => {
  let api: TestApiClient;
  let seedIssue: { id: string; title: string };

  const applyDateFromPopover = async (page: import("@playwright/test").Page, dataDay: string) => {
    const popover = page.locator('[data-slot="popover-content"]').last();
    await popover.locator(`[data-day="${dataDay}"]`).last().click();
    await popover.getByRole("button", { name: "Apply" }).click();
  };

  const normalizeIso = (value: string | null) => (value ? new Date(value).toISOString() : null);

  test.beforeEach(async ({ page }, testInfo) => {
    api = await createTestApi(testInfo.parallelIndex);
    seedIssue = await api.createIssue("E2E Seed Issue " + Date.now());
    await loginAsDefault(page, testInfo.parallelIndex);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("issues page loads with list view", async ({ page }) => {
    await expect(page).toHaveURL(/\/issues/);
    await expect(page.getByRole("link", { name: "Issues", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: new RegExp(seedIssue.title) }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "View options" })).toHaveCount(0);
  });

  test("can search issues from the list page", async ({ page }) => {
    const searchableTitle = "E2E Searchable Issue " + Date.now();
    await api.createIssue(searchableTitle, {
      description: "E2E searchable description",
    });

    await page.reload();
    await page.getByPlaceholder("Search by title, description, issue number, or issue ID").fill(searchableTitle);

    await expect(page.getByText(searchableTitle).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(seedIssue.title).first()).not.toBeVisible();
  });

  test("can create a new issue", async ({ page }) => {
    await page.getByRole("button", { name: "New issue" }).click();

    const title = "E2E Created " + Date.now();
    await page.getByLabel("Issue title").fill(title);
    await page.getByRole("button", { name: "Create Issue" }).click();

    await expect(page.locator(`text=${title}`).first()).toBeVisible({
      timeout: 10000,
    });

    await page.goto("/backlog");
    await expect(page.getByText(title).first()).toBeVisible({ timeout: 10000 });
  });

  test("can manage attachments while creating an issue", async ({ page }) => {
    await page.getByRole("button", { name: "New issue" }).click();

    let fileChooserPromise = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "Upload attachment" }).click();
    let fileChooser = await fileChooserPromise;
    await fileChooser.setFiles({
      name: "draft-note.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("Draft attachment"),
    });
    await expect(page.getByLabel("Pending issue attachments")).toContainText("draft-note.txt");
    const pendingDownloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Download pending attachment" }).click();
    expect((await pendingDownloadPromise).suggestedFilename()).toBe("draft-note.txt");

    await page.getByRole("button", { name: "Rename pending attachment" }).click();
    await page.getByLabel("Pending attachment filename").fill("renamed-draft-note.txt");
    await page.getByRole("button", { name: "Save pending attachment" }).click();
    await expect(page.getByLabel("Pending issue attachments")).toContainText("renamed-draft-note.txt");

    await page.getByRole("button", { name: "Delete pending attachment" }).click();
    await expect(page.getByLabel("Pending issue attachments")).not.toBeVisible();

    fileChooserPromise = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "Upload attachment" }).click();
    fileChooser = await fileChooserPromise;
    await fileChooser.setFiles({
      name: "linked-note.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("Linked attachment"),
    });
    await expect(page.getByLabel("Pending issue attachments")).toContainText("linked-note.txt");

    const title = "E2E Create Attachment " + Date.now();
    await page.getByLabel("Issue title").fill(title);
    const linkResponse = page.waitForResponse((response) =>
      response.url().includes("/api/issues/") && response.url().includes("/attachments/link") && response.status() === 200,
    );
    await page.getByRole("button", { name: "Create Issue" }).click();
    await linkResponse;
    await expect(page.getByText(title).first()).toBeVisible({ timeout: 10000 });

    const created = await api.listIssues({ search: title });
    const issue = created.issues?.find((item: { title: string }) => item.title === title);
    expect(issue).toBeTruthy();

    await page.goto(`/issues/${issue.id}`);
    await expect(page.getByLabel("Issue attachments")).toContainText("linked-note.txt");
    const issueDownloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Download attachment" }).click();
    expect((await issueDownloadPromise).suggestedFilename()).toBe("linked-note.txt");
    await api.deleteIssue(issue.id);
  });

  test("can create an issue from mocked voice capture", async ({ page }) => {
    const transcript = `E2E Voice Capture ${Date.now()}. Details from mocked recording.`;
    let uploadBody = "";
    await page.evaluate(() => {
      Object.defineProperty(navigator, "mediaDevices", {
        configurable: true,
        value: {
          getUserMedia: async () => ({
            getTracks: () => [{ stop: () => undefined }],
          }),
        },
      });

      class MockMediaRecorder {
        static isTypeSupported() {
          return true;
        }

        mimeType = "audio/webm";
        state = "inactive";
        ondataavailable: ((event: { data: Blob }) => void) | null = null;
        onstop: (() => void) | null = null;

        start() {
          this.state = "recording";
        }

        stop() {
          this.state = "inactive";
          this.ondataavailable?.({ data: new Blob(["mock audio"], { type: this.mimeType }) });
          this.onstop?.();
        }
      }

      Object.defineProperty(window, "MediaRecorder", {
        configurable: true,
        value: MockMediaRecorder,
      });
    });

    await page.route("**/api/transcriptions", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          text: transcript,
          provider: "cloudflare",
          model: "@cf/openai/whisper-large-v3-turbo",
        }),
      });
    });
    await page.route("**/api/upload-file", async (route) => {
      uploadBody = route.request().postData() ?? "";
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          id: crypto.randomUUID(),
          issue_id: null,
          comment_id: null,
          filename: "voice.webm",
          url: "http://localhost/mock/voice.webm",
          content_type: "audio/webm",
          size_bytes: 10,
          created_at: new Date().toISOString(),
        }),
      });
    });

    await page.getByRole("button", { name: "New issue" }).click();
    await page.getByRole("button", { name: "Record voice" }).click();
    await expect(page.getByText("Recording")).toBeVisible();
    await page.getByRole("button", { name: "Stop", exact: true }).click();
    await expect(page.getByLabel("Voice transcript")).toHaveValue(transcript);
    await page.getByLabel("Keep original recording").check();
    await page.getByRole("button", { name: "Insert" }).click();

    await expect(page.getByLabel("Issue title")).toContainText(transcript.split(".")[0]);
    await page.getByRole("button", { name: "Create Issue" }).click();
    await expect(page.getByText(transcript.split(".")[0]).first()).toBeVisible({ timeout: 10000 });
    await expect.poll(() => uploadBody).toContain("issue_id");

    const created = await api.listIssues({ search: transcript.split(".")[0] });
    for (const issue of created.issues ?? []) {
      if (issue.title === transcript.split(".")[0]) {
        await api.deleteIssue(issue.id);
      }
    }
  });

  test("preserves original voice recording as an issue attachment by default", async ({ page }) => {
    const transcript = `E2E Voice Attachment ${Date.now()}. Details from mocked recording.`;
    await page.evaluate(() => {
      Object.defineProperty(navigator, "mediaDevices", {
        configurable: true,
        value: {
          getUserMedia: async () => ({
            getTracks: () => [{ stop: () => undefined }],
          }),
        },
      });

      class MockMediaRecorder {
        static isTypeSupported() {
          return true;
        }

        mimeType = "audio/webm";
        state = "inactive";
        ondataavailable: ((event: { data: Blob }) => void) | null = null;
        onstop: (() => void) | null = null;

        start() {
          this.state = "recording";
        }

        stop() {
          this.state = "inactive";
          this.ondataavailable?.({ data: new Blob(["mock audio"], { type: this.mimeType }) });
          this.onstop?.();
        }
      }

      Object.defineProperty(window, "MediaRecorder", {
        configurable: true,
        value: MockMediaRecorder,
      });
    });

    await page.route("**/api/transcriptions", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          text: transcript,
          provider: "cloudflare",
          model: "@cf/openai/whisper-large-v3-turbo",
        }),
      });
    });

    await page.getByRole("button", { name: "New issue" }).click();
    await page.getByRole("button", { name: "Record voice" }).click();
    await page.getByRole("button", { name: "Stop", exact: true }).click();
    await expect(page.getByLabel("Voice transcript")).toHaveValue(transcript);
    await expect(page.getByLabel("Keep original recording")).toBeChecked();
    await page.getByRole("button", { name: "Insert" }).click();

    const expectedTitle = transcript.split(".")[0] + ".";
    const uploadResponse = page.waitForResponse((response) =>
      response.url().includes("/api/upload-file") && response.status() === 200,
    );
    await page.getByRole("button", { name: "Create Issue" }).click();
    await uploadResponse;
    await expect(page.getByText(expectedTitle).first()).toBeVisible({ timeout: 10000 });

    const created = await api.listIssues({ search: expectedTitle });
    const issue = created.issues?.find((item: { title: string }) => item.title === expectedTitle);
    expect(issue).toBeTruthy();

    const attachments = await api.listAttachments(issue.id);
    expect(attachments).toHaveLength(1);
    expect(attachments[0].filename).toMatch(/^voice-.*\.webm$/);

    await page.getByRole("link", { name: new RegExp(expectedTitle) }).first().click();
    await expect(page.getByLabel("Issue attachments")).toContainText(attachments[0].filename);

    await api.deleteIssue(issue.id);
  });

  test("can manage issue attachments uploaded from the description editor", async ({ page }) => {
    const issue = await api.createIssue("E2E Attachment Management " + Date.now());

    await page.goto(`/issues/${issue.id}`);
    const fileChooserPromise = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "Upload attachment" }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles({
      name: "voice-note.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("Attachment uploaded from issue description"),
    });

    await expect(page.getByLabel("Issue attachments")).toContainText("voice-note.txt");

    await page.getByRole("button", { name: "Rename attachment" }).click();
    await page.getByLabel("Attachment filename").fill("renamed-voice-note.txt");
    const renameResponse = page.waitForResponse((response) =>
      response.url().includes("/api/attachments/") && response.request().method() === "PATCH" && response.status() === 200,
    );
    await page.getByRole("button", { name: "Save attachment" }).click();
    await renameResponse;
    await expect(page.getByRole("link", { name: "renamed-voice-note.txt" })).toBeVisible();

    const deleteResponse = page.waitForResponse((response) =>
      response.url().includes("/api/attachments/") && response.request().method() === "DELETE" && response.status() === 204,
    );
    await page.getByRole("button", { name: "Delete attachment" }).click();
    await deleteResponse;
    await expect(page.getByLabel("Issue attachments")).not.toBeVisible();
  });

  test("keeps issue creation when voice attachment upload fails", async ({ page }) => {
    const transcript = `E2E Voice Upload Failure ${Date.now()}. Details from mocked recording.`;
    await page.evaluate(() => {
      Object.defineProperty(navigator, "mediaDevices", {
        configurable: true,
        value: {
          getUserMedia: async () => ({
            getTracks: () => [{ stop: () => undefined }],
          }),
        },
      });

      class MockMediaRecorder {
        static isTypeSupported() {
          return true;
        }

        mimeType = "audio/webm";
        state = "inactive";
        ondataavailable: ((event: { data: Blob }) => void) | null = null;
        onstop: (() => void) | null = null;

        start() {
          this.state = "recording";
        }

        stop() {
          this.state = "inactive";
          this.ondataavailable?.({ data: new Blob(["mock audio"], { type: this.mimeType }) });
          this.onstop?.();
        }
      }

      Object.defineProperty(window, "MediaRecorder", {
        configurable: true,
        value: MockMediaRecorder,
      });
    });

    await page.route("**/api/transcriptions", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          text: transcript,
          provider: "cloudflare",
          model: "@cf/openai/whisper-large-v3-turbo",
        }),
      });
    });
    await page.route("**/api/upload-file", async (route) => {
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: "mock upload failure" }),
      });
    });

    await page.getByRole("button", { name: "New issue" }).click();
    await page.getByRole("button", { name: "Record voice" }).click();
    await page.getByRole("button", { name: "Stop", exact: true }).click();
    await expect(page.getByLabel("Voice transcript")).toHaveValue(transcript);
    await page.getByLabel("Keep original recording").check();
    await page.getByRole("button", { name: "Insert" }).click();

    const expectedTitle = transcript.split(".")[0];
    await page.getByRole("button", { name: "Create Issue" }).click();
    await expect(page.getByText(expectedTitle).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText("Issue created, but the voice recording was not preserved")).toBeVisible();

    const created = await api.listIssues({ search: expectedTitle });
    for (const issue of created.issues ?? []) {
      if (issue.title === expectedTitle) {
        await api.deleteIssue(issue.id);
      }
    }
  });

  test("backlog today and upcoming routes render derived issue views", async ({ page }) => {
    const today = new Date();
    today.setHours(12, 0, 0, 0);

    const tomorrow = new Date(today);
    tomorrow.setDate(today.getDate() + 1);

    const backlogTitle = "E2E Backlog View " + Date.now();
    const todayTitle = "E2E Today View " + Date.now();
    const upcomingTitle = "E2E Upcoming View " + Date.now();

    await api.createIssue(backlogTitle, { status: "backlog" });
    await api.createIssue(todayTitle, {
      status: "todo",
      due_date: today.toISOString(),
    });
    await api.createIssue(upcomingTitle, {
      status: "todo",
      start_date: tomorrow.toISOString(),
    });

    await page.reload();

    await page.goto("/backlog");
    await expect(page.getByText(backlogTitle).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(todayTitle).first()).not.toBeVisible();

    await page.goto("/today");
    await expect(page.getByText(todayTitle).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(upcomingTitle).first()).not.toBeVisible();

    await page.goto("/upcoming");
    await expect(page.getByText(upcomingTitle).first()).toBeVisible({ timeout: 10000 });
  });

  test("backlog view opens issue detail", async ({ page }) => {
    const title = "E2E Backlog Detail " + Date.now();
    const issue = await api.createIssue(title, { status: "backlog" });

    await page.reload();
    await page.goto("/backlog");

    const issueLink = page.getByRole("link", { name: new RegExp(title) }).first();
    await expect(issueLink).toBeVisible({ timeout: 10000 });
    await issueLink.click();

    await page.waitForURL(new RegExp(`/issues/${issue.id}$`));
    // Use .last() to target the desktop right sidebar — the first Properties
    // button is inside a mobile-only md:hidden container.
    await expect(page.getByText("Properties").last()).toBeVisible();
  });

  test("project board shows only issues linked to that project", async ({ page }) => {
    const project = await api.createProject({
      title: "E2E Project Board " + Date.now(),
      icon: "📁",
    });

    const linkedTitle = "E2E Project Board Linked " + Date.now();
    const unrelatedTitle = "E2E Project Board Other " + Date.now();

    await api.createIssue(linkedTitle, {
      status: "todo",
      project_id: project.id,
    });
    await api.createIssue(unrelatedTitle, {
      status: "todo",
    });

    await page.reload();
    await page.goto(`/projects/${project.id}/board`);

    await expect(page.getByText(linkedTitle).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(unrelatedTitle).first()).not.toBeVisible();
    // On desktop, the board title is rendered as a breadcrumb (<span>), not an <h1>.
    // The <h1> with the same text exists in the mobile-only md:hidden container.
    await expect(page.locator("span").filter({ hasText: `${project.title} Board` }).first()).toBeVisible();
  });

  test("can create and edit issue schedule dates", async ({ page }) => {
    const title = "E2E Scheduled " + Date.now();
    const schedule = await page.evaluate(() => {
      const build = (offset: number) => {
        const date = new Date();
        // The issue datetime picker defaults to 08:00 local time for new selections.
        date.setHours(8, 0, 0, 0);
        date.setDate(date.getDate() + offset);
        return {
          dataDay: date.toLocaleDateString(),
          iso: date.toISOString(),
          label: date.toLocaleDateString("en-US", { month: "short", day: "numeric" }),
        };
      };

      return {
        start: build(0),
        end: build(1),
        updatedEnd: build(2),
      };
    });

    await page.getByRole("button", { name: "New issue" }).click();
    await page.getByLabel("Issue title").fill(title);

    await page.getByRole("button", { name: "Start date" }).click();
    await applyDateFromPopover(page, schedule.start.dataDay);

    await page.getByRole("button", { name: "End date" }).click();
    await applyDateFromPopover(page, schedule.end.dataDay);

    await page.getByRole("button", { name: "Create Issue" }).click();

    const issueLink = page.getByRole("link", { name: new RegExp(title) }).first();
    await expect(issueLink).toBeVisible({ timeout: 10000 });
    await issueLink.click();

    await page.waitForURL(/\/issues\/[\w-]+/);
    const issueId = page.url().split("/").pop();
    if (!issueId) {
      throw new Error("Missing issue id from detail URL");
    }

    await expect(page.getByRole("button", { name: new RegExp(`^${schedule.start.label},`) })).toBeVisible();
    await expect(page.getByRole("button", { name: new RegExp(`^${schedule.end.label},`) })).toBeVisible();

    await expect.poll(async () => {
      const issue = await api.getIssue(issueId);
      return {
        start_date: normalizeIso(issue.start_date),
        end_date: normalizeIso(issue.end_date),
      };
    }).toEqual({
      start_date: schedule.start.iso,
      end_date: schedule.end.iso,
    });

    await page.getByRole("button", { name: new RegExp(`^${schedule.start.label},`) }).click();
    await page.getByRole("button", { name: "Clear date" }).click();
    await expect(page.getByRole("button", { name: "Start date" })).toBeVisible();

    await page.getByRole("button", { name: new RegExp(`^${schedule.end.label},`) }).click();
    await applyDateFromPopover(page, schedule.updatedEnd.dataDay);
    await expect(page.getByRole("button", { name: new RegExp(`^${schedule.updatedEnd.label},`) })).toBeVisible();

    await expect.poll(async () => {
      const issue = await api.getIssue(issueId);
      return {
        start_date: normalizeIso(issue.start_date),
        end_date: normalizeIso(issue.end_date),
      };
    }).toEqual({
      start_date: null,
      end_date: schedule.updatedEnd.iso,
    });
  });

  test("can create child issues and manage labels and dependencies", async ({ page }) => {
    const parent = await api.createIssue("E2E Parent " + Date.now());
    const blocker = await api.createIssue("E2E Blocker " + Date.now());
    await api.createIssueLabel("Backend");

    await page.reload();
    await page.getByRole("button", { name: "New issue" }).click();
    const childTitle = "E2E Child " + Date.now();
    await page.getByLabel("Issue title").fill(childTitle);
    await page.getByRole("button", { name: "No parent" }).click();
    await page.getByPlaceholder("Search parent issue...").fill(parent.title);
    await page.getByRole("button", { name: new RegExp(parent.title) }).click();
    await page.getByRole("button", { name: "Create Issue" }).click();

    const childLink = page.getByRole("link", { name: new RegExp(childTitle) }).first();
    await expect(childLink).toBeVisible({ timeout: 10000 });
    await childLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    await page.getByRole("button", { name: "No labels" }).click();
    await page.getByPlaceholder("Search or create label...").fill("Backend");
    await page.getByRole("button", { name: /^Backend$/ }).click();

    await page.getByRole("button", { name: "Blocked by" }).click();
    await page.getByPlaceholder("Add blocked by issue...").fill(blocker.title);
    await page.getByRole("button", { name: new RegExp(blocker.title) }).click();

    const issueId = page.url().split("/").pop();
    if (!issueId) {
      throw new Error("Missing issue id from detail URL");
    }

    await expect.poll(async () => {
      const issue = await api.getIssue(issueId);
      return {
        parent_issue_id: issue.parent_issue_id,
        labels: issue.labels?.map((label: { name: string }) => label.name) ?? [],
        blocked_by: issue.dependencies?.blocked_by?.map((entry: { issue: { id: string } }) => entry.issue.id) ?? [],
      };
    }).toEqual({
      parent_issue_id: parent.id,
      labels: ["Backend"],
      blocked_by: [blocker.id],
    });
  });

  test("can navigate to issue detail page", async ({ page }) => {
    const title = "E2E Detail Test " + Date.now();
    const issue = await api.createIssue(title);

    await page.reload();
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();

    const issueLink = page.getByRole("link", { name: new RegExp(title) }).first();
    await expect(issueLink).toBeVisible({ timeout: 10000 });
    await issueLink.click();

    await page.waitForURL(/\/issues\/[\w-]+/);
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    // Use .last() to target the desktop right sidebar — the first Properties
    // button is inside a mobile-only md:hidden container.
    await expect(page.getByText("Properties").last()).toBeVisible();
    await expect(page.getByRole("button", { name: "Post comment" })).toBeVisible();
  });

  test("can cancel issue creation", async ({ page }) => {
    await page.getByRole("button", { name: "New issue" }).click();
    await expect(page.getByLabel("Issue title")).toBeVisible();
    await page.getByRole("button", { name: "Close new issue dialog" }).click();
    await expect(page.getByLabel("Issue title")).not.toBeVisible();
    await expect(page.getByRole("button", { name: "New issue" })).toBeVisible();
  });

  test("board route keeps the board page", async ({ page }) => {
    await page.goto("/board");
    await page.waitForURL("**/board");

    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    await expect(page.getByText("Backlog").first()).toBeVisible();
    await expect(page.getByRole("button", { name: "View options" })).toHaveCount(
      0,
    );
  });
});
