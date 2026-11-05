// @vitest-environment jsdom
import { act,type ComponentProps } from "react";
import { createRoot,type Root } from "react-dom/client";
import { afterEach,beforeEach,expect,it,vi } from "vitest";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import type {
AgentGuiHostEvent,
AgentGuiHostServices,
AgentGuiOperationInvoker,
LocalConversationSource
} from "./protocol";
import { UsageIndicator } from "./usage-indicator";

let root: Root;
let container: HTMLDivElement;
const listeners = new Set<(event: AgentGuiHostEvent) => void>();
type Request = (
  method: string,
  params: Record<string, unknown>
) => Promise<unknown>;
let request: ReturnType<typeof vi.fn<Request>>;
let client: AgentGuiHostServices;
const usage = {
  provider: "codex",
  context: { usedTokens: 100, limitTokens: 1000, model: null, updatedAt: null },
  account: { status: "available", plan: "pro", windows: [], updatedAt: null }
};
beforeEach(() => {
  turns.clear();
  observers.clear();
  refresh = vi.fn(async () => {});
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  );
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  request = vi.fn<Request>(async (method, params) =>
    method === "usage.read"
      ? usage
      : method === "session.compact"
        ? { turnId: `turn-${params.requestId}` }
        : undefined
  );
  client = fakeHostServices(request as AgentGuiOperationInvoker, (listener) => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  });
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  listeners.clear();
  vi.unstubAllGlobals();
});
const button = (label: string) =>
  document.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!;
const turns = new Map<string, { status: string; error: unknown }>();
const observers = new Set<() => void>();
let refresh = vi.fn(async () => {});
const localConversations = {
  observe: (_id: string, listener: () => void) => {
    observers.add(listener);
    return () => {
      observers.delete(listener);
    };
  },
  conversation: () => ({ turns }),
  refresh: () => refresh()
} as unknown as LocalConversationSource;
async function renderUsage(
  props: Partial<ComponentProps<typeof UsageIndicator>> = {}
) {
  await act(async () =>
    root.render(
      <UsageIndicator
        client={client}
        localConversations={localConversations}
        agentId="local:codex"
        sessionId="conversation"
        chinese={false}
        busy={false}
        active
        {...props}
      />
    )
  );
}
async function open() {
  await act(async () => button("Usage").click());
}
async function complete(status = "completed", sessionId = "conversation") {
  const id = request.mock.calls.find(
    ([method]) => method === "session.compact"
  )![1].requestId;
  await act(async () => {
    turns.set(`turn-${id}`, {
      status,
      error: status === "failed" ? { message: "Not enough history" } : undefined
    });
    // A left conversation's observer is gone, so its completion reaches nothing.
    if (sessionId === "conversation")
      for (const observer of [...observers]) observer();
  });
}

it.each([
  [0, "0"],
  [999, "999"],
  [1000, "1k"],
  [1049, "1k"],
  [1050, "1.1k"],
  [10049, "10k"],
  [10050, "10.1k"],
  [65918, "65.9k"],
  [785685, "785.7k"],
  [999999, "1000k"],
  [1000000, "1000k"],
  [1234567, "1234.6k"]
])(
  "shortens %s context tokens to %s without a reported limit",
  async (usedTokens, expected) => {
    request.mockResolvedValue({
      ...usage,
      context: { ...usage.context, usedTokens, limitTokens: null }
    });
    await renderUsage();
    expect(button("Usage").textContent).toBe(`Context ${expected}`);
  }
);

it.each([false, true])(
  "keeps exact tokens in details and account remaining alongside the compact summary (Chinese: %s)",
  async (chinese) => {
    request.mockResolvedValue({
      ...usage,
      provider: "claude",
      context: { ...usage.context, usedTokens: 65918, limitTokens: null },
      account: {
        ...usage.account,
        windows: [{ id: "five_hour", label: "5h", usedPercent: 44 }]
      }
    });
    await renderUsage({ chinese, agentId: "local:claude" });
    const trigger = button(chinese ? "用量" : "Usage");
    expect(trigger.textContent).toBe(
      chinese ? "上下文 65.9k剩余 56%" : "Context 65.9kleft 56%"
    );
    await act(async () => trigger.click());
    expect(document.querySelector(".multica-usage-detail")?.textContent).toBe(
      `65,918 · ${chinese ? "上限未提供" : "Limit not reported"} tokens`
    );
  }
);

it("retains percentage summaries and exact used/limit details when the context limit is known", async () => {
  await renderUsage();
  expect(button("Usage").textContent).toBe("Context 10%");
  await open();
  expect(document.querySelector(".multica-usage-detail")?.textContent).toBe(
    "100 / 1,000 tokens"
  );
});

it("retains the empty-data label when no context count is available", async () => {
  request.mockResolvedValue({
    ...usage,
    context: { ...usage.context, usedTokens: null, limitTokens: null }
  });
  await renderUsage();
  expect(button("Usage").textContent).toBe("Usage");
});

it("waits for native completion, prevents duplicate clicks and refreshes usage after compaction", async () => {
  await renderUsage();
  await open();
  await act(async () => {
    button("Compact").click();
    button("Compact").click();
  });
  expect(
    request.mock.calls.filter(([method]) => method === "session.compact")
  ).toHaveLength(1);
  expect(button("Compact").disabled).toBe(true);
  expect(document.body.textContent).not.toContain(
    "Conversation context compacted"
  );
  await complete("completed", "another-conversation");
  expect(button("Compact").disabled).toBe(true);
  await complete();
  expect(button("Compact").disabled).toBe(false);
  expect(document.body.textContent).toContain("Conversation context compacted");
  expect(request).toHaveBeenCalledWith(
    "usage.read",
    expect.objectContaining({ sessionId: "conversation", force: true })
  );
});

it.each([
  { sessionId: null },
  { busy: true },
  { disabled: true },
  { active: false }
])(
  "disables compaction for $sessionId $busy $disabled $active",
  async (props) => {
    await renderUsage(props);
    await open();
    expect(button("Compact").disabled).toBe(true);
    await act(async () => button("Compact").click());
    expect(
      request.mock.calls.some(([method]) => method === "session.compact")
    ).toBe(false);
  }
);

it("shows provider errors and allows an explicit retry", async () => {
  await renderUsage();
  await open();
  await act(async () => button("Compact").click());
  await complete("failed");
  expect(document.querySelector('[role="alert"]')?.textContent).toBe(
    "Not enough history"
  );
  expect(button("Compact").disabled).toBe(false);
});

it("discards completion from a conversation the user has left", async () => {
  await renderUsage();
  await open();
  await act(async () => button("Compact").click());
  await renderUsage({ sessionId: "different" });
  await complete();
  expect(document.body.textContent).not.toContain(
    "Conversation context compacted"
  );
  expect(
    request.mock.calls.some(([method]) => method === "host.showMessage")
  ).toBe(false);
});

it("keeps a rejected command visible as an error", async () => {
  await renderUsage();
  await open();
  const original = request.getMockImplementation()!;
  request.mockImplementation((method, params) =>
    method === "session.compact"
      ? Promise.reject(new Error("Runtime unavailable"))
      : original(method, params)
  );
  await act(async () => button("Compact").click());
  expect(document.querySelector('[role="alert"]')?.textContent).toBe(
    "Runtime unavailable"
  );
  expect(button("Compact").disabled).toBe(false);
});

it.each([false, true])(
  "reads durable completion when the event was missed (reconnect: %s)",
  async (reconnect) => {
    const finish = () =>
      turns.set(
        `turn-${request.mock.calls.find(([name]) => name === "session.compact")![1].requestId}`,
        { status: "completed", error: undefined }
      );
    if (!reconnect)
      refresh = vi.fn(async () => {
        finish();
      });
    await renderUsage();
    await open();
    await act(async () => button("Compact").click());
    if (reconnect) {
      expect(button("Compact").disabled).toBe(true);
      await act(async () => {
        finish();
        for (const observer of [...observers]) observer();
      });
    }
    expect(button("Compact").disabled).toBe(false);
    expect(document.body.textContent).toContain(
      "Conversation context compacted"
    );
  }
);
