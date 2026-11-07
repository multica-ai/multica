// @vitest-environment jsdom
import { act,type ButtonHTMLAttributes } from "react";
import { createRoot,type Root } from "react-dom/client";
import { afterEach,beforeEach,expect,it,vi } from "vitest";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import type {
AgentGuiHostServices,
AgentGuiOperationInvoker
} from "./protocol";
import { ResourceLinkProvider,ResourceMarkdownLink } from "./resource-link";
import { SideEntryProvider } from "./side-entry";

vi.mock("@tutti-os/ui-system", () => ({ DropdownMenuItem: () => null }));
vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    onClick,
    disabled,
    "aria-busy": busy
  }: ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button onClick={onClick} disabled={disabled} aria-busy={busy}>
      {children}
    </button>
  )
}));

let container: HTMLDivElement;
let root: Root;
const request = vi.fn<AgentGuiOperationInvoker>();
const writeText = vi.fn<(text: string) => Promise<void>>();
const client: AgentGuiHostServices = fakeHostServices(request, () => () => {});
const handle = {} as Parameters<typeof SideEntryProvider>[0]["handle"];

function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((complete, fail) => {
    resolve = complete;
    reject = fail;
  });
  return { promise, resolve, reject };
}

async function mount(chinese = true, onAddQuote?: Parameters<typeof SideEntryProvider>[0]["onAddQuote"]) {
  await act(async () =>
    root.render(
      <SideEntryProvider
        root={container}
        client={client}
        handle={handle}
        sessionId="session-a"
        chinese={chinese}
        enabled
        onAddQuote={onAddQuote}
        quoteSourceName="Claude Code"
      >
        <article data-multica-turn-id="turn-a">
          <p>Selected reply text</p>
        </article>
      </SideEntryProvider>
    )
  );
}

async function select() {
  const range = document.createRange();
  range.selectNodeContents(container.querySelector("p")!);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(range);
  await act(async () =>
    document.dispatchEvent(new Event("pointerup", { bubbles: true }))
  );
}

async function click(index: number) {
  await act(async () => container.querySelectorAll("button")[index]!.click());
}

async function expectDismissed() {
  expect(container.querySelector('[role="toolbar"]')).toBeNull();
  expect(window.getSelection()!.rangeCount).toBe(0);
  await act(async () => {
    document.dispatchEvent(new Event("pointerup"));
    document.dispatchEvent(new Event("scroll"));
    window.dispatchEvent(new Event("resize"));
  });
  expect(container.querySelector('[role="toolbar"]')).toBeNull();
}

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  Object.defineProperty(Range.prototype, "getBoundingClientRect", {
    configurable: true,
    value: () => new DOMRect(100, 100, 200, 20)
  });
  vi.stubGlobal(
    "navigator",
    Object.create(navigator, { clipboard: { value: { writeText } } })
  );
  request.mockReset().mockResolvedValue(undefined);
  writeText.mockReset().mockResolvedValue(undefined);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  window.getSelection()?.removeAllRanges();
  container.remove();
  Reflect.deleteProperty(Range.prototype, "getBoundingClientRect");
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("adds the selection to the current composer with its actual author and dismisses without sending", async () => {
  const add = vi.fn();
  await mount(true, add);
  container.querySelector("article")!.dataset.multicaQuoteSource = "Review agent";
  await select();
  expect(Array.from(container.querySelectorAll("button"), (button) => button.textContent)).toEqual([
    "复制", "添加到输入框", "在旁支中询问"
  ]);
  await click(1);
  expect(add).toHaveBeenCalledExactlyOnceWith({
    sessionId: "session-a", text: "Selected reply text", sourceName: "Review agent"
  });
  await expectDismissed();
  expect(request).not.toHaveBeenCalled();
});

it.each([
  [true, "复制成功"],
  [false, "Copied successfully"]
] as const)(
  "dismisses immediately and reports copy success after writing (Chinese: %s)",
  async (chinese, message) => {
    const copy = deferred();
    writeText.mockReturnValue(copy.promise);
    await mount(chinese);
    await select();
    await click(0);
    expect(writeText).toHaveBeenCalledExactlyOnceWith("Selected reply text");
    await expectDismissed();
    expect(request).not.toHaveBeenCalled();
    await act(async () => copy.resolve());
    expect(request).toHaveBeenCalledExactlyOnceWith("host.showMessage", {
      message,
      tone: "success"
    });
    await select();
    expect(container.querySelector('[role="toolbar"]')).not.toBeNull();
  }
);

it.each([
  [true, "无法复制文字，请重试"],
  [false, "Could not copy text. Try again."]
] as const)(
  "reports a failed copy without success feedback (Chinese: %s)",
  async (chinese, message) => {
    writeText.mockRejectedValue(new Error("Clipboard denied"));
    await mount(chinese);
    await select();
    await click(0);
    await expectDismissed();
    expect(request).toHaveBeenCalledExactlyOnceWith("host.showMessage", {
      message,
      tone: "error"
    });
  }
);

it.each(["resolve", "reject"] as const)(
  "dismisses before a side request completes and preserves pending protection (%s)",
  async (result) => {
    const side = deferred();
    request.mockImplementation((method) =>
      method === "host.openSide" ? side.promise : Promise.resolve(undefined)
    );
    await mount();
    await select();
    await click(1);
    await expectDismissed();
    expect(request).toHaveBeenCalledExactlyOnceWith("host.openSide", {
      sessionId: "session-a",
      turnId: "turn-a",
      quote: "Selected reply text",
      requestId: expect.any(String)
    });
    await select();
    expect(container.querySelectorAll("button")[1]!.disabled).toBe(true);
    await click(1);
    expect(request).toHaveBeenCalledTimes(1);
    await act(async () => {
      if (result === "reject") side.reject(new Error("Side unavailable"));
      else side.resolve();
    });
    expect(container.querySelectorAll("button")[1]!.disabled).toBe(false);
    if (result === "reject")
      expect(request).toHaveBeenLastCalledWith("host.showMessage", {
        message: "Side unavailable",
        tone: "error"
      });
    else expect(request).toHaveBeenCalledTimes(1);
  }
);

it("forwards the right-clicked local href and source session without using the selected text", async () => {
  await mount();
  await select();
  const link = document.createElement("a");
  link.href = "/tmp/测试%20build.dmg";
  link.innerHTML = "<span>Download</span>";
  container.querySelector("article")!.append(link);
  const event = new MouseEvent("contextmenu", {
    bubbles: true,
    cancelable: true,
    clientX: 40,
    clientY: 80
  });
  await act(async () => {
    link.firstElementChild!.dispatchEvent(event);
  });
  expect(event.defaultPrevented).toBe(true);
  expect(request).toHaveBeenCalledExactlyOnceWith("host.showFileContextMenu", {
    sessionId: "session-a",
    url: "/tmp/测试%20build.dmg",
    hasSelection: true
  });
});

it.each(["click", "keydown", "contextmenu"])(
  "leaves %s on links outside the GUI to their own handler",
  async (type) => {
    await mount();
    const link = document.createElement("a");
    link.href =
      type === "contextmenu"
        ? "/project/file.ts"
        : "https://multica.ai/workspace/conversations?provider=codex&session=other";
    link.innerHTML = "<span>Another conversation</span>";
    document.body.append(link);
    const navigate = vi.fn((event: Event) => event.preventDefault());
    link.addEventListener(type, navigate);
    try {
      const event =
        type === "keydown"
          ? new KeyboardEvent(type, {
              key: "Enter",
              bubbles: true,
              cancelable: true
            })
          : new MouseEvent(type, {
              button: 0,
              bubbles: true,
              cancelable: true
            });
      await act(async () => {
        link.firstElementChild!.dispatchEvent(event);
      });
      expect(navigate).toHaveBeenCalledOnce();
      expect(request).not.toHaveBeenCalled();
    } finally {
      link.remove();
    }
  }
);

it.each(["click", "keydown"])(
  "previews a resource inside the GUI on %s",
  async (type) => {
    await mount();
    const link = document.createElement("a");
    link.href = "https://example.com/document";
    link.innerHTML = "<span>Documentation</span>";
    container.querySelector("article")!.append(link);
    const event =
      type === "keydown"
        ? new KeyboardEvent(type, {
            key: "Enter",
            bubbles: true,
            cancelable: true
          })
        : new MouseEvent(type, { button: 0, bubbles: true, cancelable: true });
    await act(async () => {
      link.firstElementChild!.dispatchEvent(event);
    });
    expect(event.defaultPrevented).toBe(true);
    expect(request).toHaveBeenCalledExactlyOnceWith("host.openResource", {
      sessionId: "session-a",
      url: link.href
    });
  }
);

it("routes a resource to its own GUI when two conversation panels are mounted", async () => {
  await mount();
  const otherContainer = document.createElement("div");
  document.body.append(otherContainer);
  const otherRoot = createRoot(otherContainer);
  const otherRequest = vi
    .fn<AgentGuiOperationInvoker>()
    .mockResolvedValue(undefined);
  try {
    await act(async () =>
      otherRoot.render(
        <SideEntryProvider
          root={otherContainer}
          client={fakeHostServices(otherRequest, () => () => {})}
          handle={handle}
          sessionId="session-b"
          chinese
          enabled
        >
          <a href="https://example.com/document">
            <span>Documentation</span>
          </a>
        </SideEntryProvider>
      )
    );
    await act(async () => {
      otherContainer.querySelector("span")!.click();
    });
    expect(request).not.toHaveBeenCalled();
    expect(otherRequest).toHaveBeenCalledExactlyOnceWith("host.openResource", {
      sessionId: "session-b",
      url: "https://example.com/document"
    });
  } finally {
    await act(async () => otherRoot.unmount());
    otherContainer.remove();
  }
});

it.each(["https://example.com/download", "mention://issue/abc"])(
  "leaves non-local context menus intact: %s",
  async (href) => {
    await mount();
    const link = document.createElement("a");
    link.href = href;
    container.append(link);
    const event = new MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true
    });
    await act(async () => {
      link.dispatchEvent(event);
    });
    expect(event.defaultPrevented).toBe(false);
    expect(request).not.toHaveBeenCalled();
  }
);

it.each([
  "issue",
  "agent",
  "project",
  "squad",
  "skill",
  "autopilot",
  "runtime"
])("preserves the context menu of a rendered %s reference", async (kind) => {
  const id = "01a186cf-49de-7308-b61e-7201c9849b94";
  await act(async () =>
    root.render(
      <ResourceLinkProvider
        client={client}
        scope={{ workspaceId: "workspace", workspaceSlug: "chovy-multica" }}
        agents={[]}
      >
        <SideEntryProvider
          root={document.body}
          client={client}
          handle={handle}
          sessionId="session-a"
          chinese
          enabled
        >
          <ResourceMarkdownLink
            href={`mention://${kind}/${id}`}
            fallback={null}
          >
            CHOV-50
          </ResourceMarkdownLink>
        </SideEntryProvider>
      </ResourceLinkProvider>
    )
  );
  const link = container.querySelector("a")!;
  expect(link.getAttribute("href")).toMatch(/^\/chovy-multica\//);
  const target = link.querySelector("span")!;
  expect(target).not.toBeNull();
  const event = new MouseEvent("contextmenu", {
    bubbles: true,
    cancelable: true
  });
  await act(async () => {
    target.dispatchEvent(event);
  });
  expect(request).not.toHaveBeenCalled();
  expect(event.defaultPrevented).toBe(false);

  await act(async () => {
    target.dispatchEvent(
      new MouseEvent("click", { bubbles: true, cancelable: true })
    );
  });
  expect(request).toHaveBeenCalledExactlyOnceWith("host.openInternalResource", {
    href: link.getAttribute("href"),
    background: false
  });
  request.mockClear();
  await act(async () => {
    target.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        bubbles: true,
        cancelable: true
      })
    );
  });
  expect(request).toHaveBeenCalledExactlyOnceWith("host.openInternalResource", {
    href: link.getAttribute("href"),
    background: false
  });
});

it("shows file actions without a selection and reports lookup failures", async () => {
  request.mockRejectedValueOnce(new Error("File not found"));
  await mount();
  const link = document.createElement("a");
  link.setAttribute("href", "./archive.zip");
  container.append(link);
  await act(async () => {
    link.dispatchEvent(
      new MouseEvent("contextmenu", { bubbles: true, cancelable: true })
    );
  });
  expect(request).toHaveBeenNthCalledWith(1, "host.showFileContextMenu", {
    sessionId: "session-a",
    url: "./archive.zip",
    hasSelection: false
  });
  expect(request).toHaveBeenLastCalledWith("host.showMessage", {
    message: "File not found",
    tone: "error"
  });
});

it.each(["image", "editor"])(
  "preserves the native %s context menu",
  async (kind) => {
    await mount();
    const wrapper = document.createElement("div");
    if (kind === "editor") wrapper.setAttribute("contenteditable", "true");
    wrapper.innerHTML =
      '<a href="/tmp/archive.zip">Download<img alt="preview" /></a>';
    container.append(wrapper);
    const target =
      kind === "image"
        ? wrapper.querySelector("img")!
        : wrapper.querySelector("a")!;
    const event = new MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true
    });
    await act(async () => {
      target.dispatchEvent(event);
    });
    expect(event.defaultPrevented).toBe(false);
    expect(request).not.toHaveBeenCalled();
  }
);
