import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { ApiError } from "@multica/core/api";
import { configureShortcutPlatform } from "@multica/core/shortcuts";
import { renderWithI18n } from "../../test/i18n";
import { WakeupCreate } from "./wakeup-create";

const create = vi.fn();
vi.mock("./wakeup-condition-names", () => ({
  useConditionNames: () => ({ status: (key: string) => key, label: () => undefined, property: () => undefined, actor: (_type: string, id: string) => id }),
}));
vi.mock("@multica/core/issues", () => ({
  useCreateIssueWakeup: () => ({ mutateAsync: create, isPending: false }),
  childIssuesOptions: () => ({ queryKey: ["children"] }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: ({ queryKey }: { queryKey: unknown[] }) => ({
    data: JSON.stringify(queryKey).includes("members")
      ? [{ user_id: "user-j", name: "Jiayuan" }]
      : [
          { id: "emacs", name: "Emacs", archived_at: null, runtime_id: "rt" },
          { id: "grok", name: "Grok", archived_at: null, runtime_id: "rt" },
        ],
  }),
}));
vi.mock("@multica/core/agents", () => ({ isAgentRuntimeBound: () => true }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../common/use-viewing-timezone", () => ({ useViewingTimezone: () => "Asia/Shanghai" }));

async function renderForm(defaultAgentId = "emacs") {
  renderWithI18n(<WakeupCreate workspaceId="ws" issueId="issue" defaultAgentId={defaultAgentId} />, { locale: "zh-Hans" });
  fireEvent.click(screen.getByRole("button", { name: "新建唤醒" }));
  await screen.findByRole("form", { name: "新建唤醒" });
}

async function chooseCondition(label: string) {
  fireEvent.click(screen.getByRole("button", { name: /选择条件/ }));
  fireEvent.click(await screen.findByRole("menuitem", { name: new RegExp(label) }));
}

beforeEach(() => {
  create.mockReset().mockResolvedValue(undefined);
  // The send shortcut is the primary modifier + Enter; pin the platform so
  // Cmd means primary on every CI runner.
  configureShortcutPlatform("macos");
});
afterEach(() => configureShortcutPlatform(null));

describe("WakeupCreateForm", () => {
  it("asks for a condition before creating anything", async () => {
    await renderForm();
    fireEvent.click(screen.getByRole("button", { name: /创建/ }));
    expect(screen.getByRole("alert")).toHaveTextContent("请选择条件");
    expect(create).not.toHaveBeenCalled();
  });

  it("creates a reply wait for the assignee with a deadline and timeout action", async () => {
    await renderForm();
    await chooseCondition("有人回复");
    expect(screen.getByRole("radiogroup", { name: "触发次数" })).toBeVisible();
    expect(screen.getByRole("button", { name: /最多等待: 7 天/ })).toBeVisible();
    expect(screen.getByRole("button", { name: /超时后: 唤醒 Emacs 处理/ })).toBeVisible();
    fireEvent.change(screen.getByLabelText("唤醒后要做什么"), { target: { value: "按回复继续实现阶段 2" } });
    fireEvent.click(screen.getByRole("radio", { name: "重复唤醒" }));
    // A repeating wait gets a cap on how many runs it may start.
    expect(screen.getByRole("button", { name: /最多触发: 20 次/ })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: /创建/ }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith({
        agent_id: "emacs",
        instruction: "按回复继续实现阶段 2",
        kind: "event",
        mode: "continuous",
        max_fires: 20,
        event_types: ["comment.created"],
        expires_in_seconds: 604800,
        on_timeout: "wake",
      }),
    );
    await waitFor(() => expect(screen.queryByRole("form", { name: "新建唤醒" })).toBeNull());
  });

  it("requires an agent when the issue has no agent assignee", async () => {
    await renderForm("");
    await chooseCondition("在某个时间");
    fireEvent.change(screen.getByLabelText("唤醒后要做什么"), { target: { value: "看一下 CI" } });
    fireEvent.click(screen.getByRole("button", { name: /创建/ }));
    expect(screen.getByRole("alert")).toHaveTextContent("请选择要唤醒的智能体");
  });

  it("gives a recurring check an end date and no timeout choice", async () => {
    await renderForm();
    await chooseCondition("定期检查");
    expect(screen.getByLabelText("截止")).toBeVisible();
    expect(screen.queryByRole("radiogroup")).toBeNull();
    fireEvent.change(screen.getByLabelText("唤醒后要做什么"), { target: { value: "巡检迁移任务" } });
    fireEvent.click(screen.getByRole("button", { name: /创建/ }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(expect.objectContaining({ kind: "cron", cron_expression: "0 9 * * *", timezone: "Asia/Shanghai", mode: "continuous" })),
    );
  });

  it("keeps the draft and explains a capacity refusal", async () => {
    create.mockRejectedValue(new ApiError("full", 400, "Bad Request", { code: "wakeup_capacity_exceeded" }));
    await renderForm();
    await chooseCondition("在某个时间");
    fireEvent.change(screen.getByLabelText("唤醒后要做什么"), { target: { value: "看一下 CI" } });
    fireEvent.click(screen.getByRole("button", { name: /创建/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("已达上限");
    expect(screen.getByLabelText("唤醒后要做什么")).toHaveValue("看一下 CI");
    expect(screen.getByRole("form", { name: "新建唤醒" })).toBeVisible();
  });

  it("submits with Cmd+Enter", async () => {
    await renderForm();
    await chooseCondition("在某个时间");
    const input = screen.getByLabelText("唤醒后要做什么");
    fireEvent.change(input, { target: { value: "看一下 CI" } });
    fireEvent.keyDown(input, { key: "Enter", metaKey: true });
    await waitFor(() => expect(create).toHaveBeenCalledWith(expect.objectContaining({ kind: "at", mode: "once" })));
  });
});

describe("platform conditions in the form", () => {
  it("groups conditions by what they wait for", async () => {
    await renderForm();
    fireEvent.click(screen.getByRole("button", { name: /选择条件/ }));
    for (const label of ["字段变为某个值", "子任务完成", "关联 PR 的 CI 结束", "其他任务的状态变化"]) {
      expect(await screen.findByRole("menuitem", { name: new RegExp(label) })).toBeVisible();
    }
    for (const group of ["协作", "运行与子任务", "关联"]) expect(screen.getByText(group)).toBeVisible();
  });

  it("creates a sub-issue wait that the platform checks", async () => {
    await renderForm();
    await chooseCondition("子任务完成");
    expect(screen.getByRole("button", { name: /子任务完成: 全部子任务/ })).toBeVisible();
    fireEvent.change(screen.getByLabelText("唤醒后要做什么"), { target: { value: "汇总子任务结果" } });
    fireEvent.click(screen.getByRole("button", { name: /创建/ }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(
        expect.objectContaining({ kind: "event", mode: "once", condition: { type: "children_done" }, expires_in_seconds: 604800 }),
      ),
    );
    expect(create.mock.calls[0]![0]).not.toHaveProperty("event_types");
  });

  it("waits for a linked PR's CI by default and can wait for the merge instead", async () => {
    await renderForm();
    await chooseCondition("关联 PR 的 CI 结束");
    fireEvent.click(screen.getByRole("button", { name: /关联 PR 的 CI 结束: CI 结束/ }));
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "合并" }));
    fireEvent.change(screen.getByLabelText("唤醒后要做什么"), { target: { value: "发布" } });
    fireEvent.click(screen.getByRole("button", { name: /创建/ }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(expect.objectContaining({ condition: { type: "pull_request", event: "merged" } })),
    );
  });

  it("asks for the value a field has to reach", async () => {
    await renderForm();
    await chooseCondition("字段变为某个值");
    fireEvent.change(screen.getByLabelText("唤醒后要做什么"), { target: { value: "继续" } });
    fireEvent.click(screen.getByRole("button", { name: /创建/ }));
    expect(screen.getByRole("alert")).toHaveTextContent("请选择一个值");
    expect(create).not.toHaveBeenCalled();
  });
});
