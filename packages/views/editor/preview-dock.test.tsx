import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  fireEvent,
  render as rtlRender,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactElement } from "react";
import type { Attachment } from "@multica/core/types";

const { getAttachmentMock } = vi.hoisted(() => ({ getAttachmentMock: vi.fn() }));

vi.mock("../platform", () => ({ openExternal: vi.fn() }));

vi.mock("@multica/core/api", () => ({
  api: {
    getBaseUrl: () => "",
    getAttachmentTextContent: vi.fn(),
    getAttachment: getAttachmentMock,
  },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("./use-download-attachment", () => ({
  useDownloadAttachment: () => vi.fn(),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("./readonly-content", () => ({ ReadonlyContent: () => null }));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const STRINGS: Record<string, Record<string, string>> = {
  image: {
    download: "Download",
    canvas_label: "Image canvas",
    copy_image: "Copy image",
  },
  canvas: {
    zoom_in: "Zoom in",
    zoom_out: "Zoom out",
    zoom_fit: "Fit to view",
    zoom_actual: "Actual size",
  },
  attachment: {
    close: "Close",
    full_screen: "Full screen",
    preview_pane: "Preview of {{filename}}",
    resize_preview: "Resize preview",
    more_actions: "More actions",
    open_in_new_tab: "Open in new tab",
    previous: "Previous",
    next: "Next",
    sequence_position: "{{index}} / {{total}}",
    preview_loading: "Loading preview...",
  },
};

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (
      sel: (s: Record<string, Record<string, string>>) => string,
      params?: Record<string, string | number>,
    ) => {
      const raw = sel(STRINGS);
      if (!params) return raw;
      return raw.replace(/\{\{(\w+)\}\}/g, (_, k: string) => String(params[k]));
    },
  }),
}));

import {
  PreviewSequenceProvider,
  collectPreviewSequence,
  usePreviewActiveKey,
  usePreviewSequence,
} from "./preview-sequence-context";
import {
  PREVIEW_MAIN_MIN_WIDTH,
  PREVIEW_PANE_DEFAULT_WIDTH,
  PREVIEW_PANE_MIN_WIDTH,
  PreviewDock,
  PreviewDockHost,
  fitPaneWidth,
  type PreviewDockYield,
} from "./preview-dock";

function render(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const UUID = (n: number) =>
  `${String(n).repeat(8)}-${String(n).repeat(4)}-4${String(n).repeat(3)}-8${String(n).repeat(3)}-${String(n).repeat(12)}`;

function imageAttachment(n: number): Attachment {
  const id = UUID(n);
  return {
    id,
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u-1",
    filename: `shot-${n}.png`,
    url: `https://cdn.example.test/${id}.png`,
    download_url: `https://cdn.example.test/${id}.png?Signature=s`,
    markdown_url: `https://cdn.example.test/${id}.png`,
    content_type: "image/png",
    size_bytes: 10,
    created_at: "2026-08-05T00:00:00Z",
  };
}

const THREE = [imageAttachment(1), imageAttachment(2), imageAttachment(3)];
const ITEMS = collectPreviewSequence(THREE.map((a) => ({ attachments: [a] })));

// jsdom lays nothing out: the dock's root reports whatever room a test gives
// it, everything else reports no size.
let dockWidth = 0;
const realRect = HTMLElement.prototype.getBoundingClientRect;
beforeEach(() => {
  dockWidth = 0;
  localStorage.clear();
  getAttachmentMock.mockImplementation(async (id: string) => imageAttachment(Number(id[0])));
  HTMLElement.prototype.getBoundingClientRect = function (this: HTMLElement) {
    const width = this.dataset.slot === "preview-dock" ? dockWidth : 0;
    return { x: 0, y: 0, top: 0, left: 0, bottom: 0, right: width, width, height: 0, toJSON: () => ({}) } as DOMRect;
  };
});
afterEach(() => {
  HTMLElement.prototype.getBoundingClientRect = realRect;
});

function fakeYield(name: string, width: number, log: string[]): PreviewDockYield {
  let parked = false;
  return {
    reclaimable: () => (parked ? 0 : width),
    park: () => {
      parked = true;
      log.push(`park ${name}`);
    },
    restore: () => {
      if (!parked) return;
      parked = false;
      log.push(`restore ${name}`);
    },
  };
}

function Opener({ openKey, label = "open" }: { openKey: string; label?: string }) {
  const sequence = usePreviewSequence();
  return (
    <button type="button" onClick={() => sequence.openAt(openKey)}>
      {label}
    </button>
  );
}

function ActiveKey() {
  return <output data-testid="active">{usePreviewActiveKey() ?? "none"}</output>;
}

function Surface({
  yields = [],
  scopeKey,
}: {
  yields?: PreviewDockYield[];
  scopeKey?: string;
}) {
  return (
    <PreviewDockHost>
      <PreviewSequenceProvider items={ITEMS} scopeKey={scopeKey}>
        <PreviewDock yields={yields}>
          <Opener openKey={THREE[1]!.id} />
          <Opener openKey={THREE[2]!.id} label="open third" />
          <ActiveKey />
        </PreviewDock>
      </PreviewSequenceProvider>
    </PreviewDockHost>
  );
}

function pane() {
  return screen.queryByRole("region", { name: /^Preview of / });
}

describe("fitPaneWidth", () => {
  it("keeps the content at its minimum and the pane between its bounds", () => {
    expect(fitPaneWidth(PREVIEW_PANE_DEFAULT_WIDTH, 1400)).toBe(PREVIEW_PANE_DEFAULT_WIDTH);
    // 1056 of room leaves the pane 456 once the content has its 600.
    expect(fitPaneWidth(PREVIEW_PANE_DEFAULT_WIDTH, 1056)).toBe(1056 - PREVIEW_MAIN_MIN_WIDTH);
    expect(fitPaneWidth(200, 1400)).toBe(PREVIEW_PANE_MIN_WIDTH);
    // Not even the narrowest pane fits: folded away.
    expect(fitPaneWidth(PREVIEW_PANE_DEFAULT_WIDTH, PREVIEW_MAIN_MIN_WIDTH + PREVIEW_PANE_MIN_WIDTH - 1)).toBe(0);
  });
});

describe("PreviewDock", () => {
  it("opens the file in a pane beside the page when there is room", () => {
    dockWidth = 1300;
    render(<Surface />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });

    expect(pane()).toHaveAccessibleName("Preview of shot-2.png");
    expect(screen.getByText("2 / 3")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByTestId("active")).toHaveTextContent(THREE[1]!.id);
  });

  it("opens the full viewer when even every yield leaves no room for a pane", () => {
    dockWidth = 700;
    const log: string[] = [];
    render(<Surface yields={[fakeYield("nav", 200, log)]} />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });

    expect(screen.getByRole("dialog")).toHaveAttribute("aria-label", "shot-2.png");
    expect(pane()).not.toBeInTheDocument();
    expect(log).toEqual([]);
    // The viewer covers the page: no card is marked.
    expect(screen.getByTestId("active")).toHaveTextContent("none");
  });

  it("asks yields in order only until the pane fits, and gives them back in reverse", async () => {
    dockWidth = 1000;
    const log: string[] = [];
    render(<Surface yields={[fakeYield("nav", 280, log), fakeYield("sidebar", 320, log)]} />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    // 1000 + 280 fits 600 + 560: the sidebar stays.
    expect(log).toEqual(["park nav"]);

    act(() => {
      fireEvent.click(screen.getByRole("button", { name: "Close" }));
    });
    expect(log).toEqual(["park nav", "restore nav"]);
    await waitFor(() => expect(pane()).not.toBeInTheDocument());
  });

  it("parks the nav and then the sidebar when the nav alone is not enough", () => {
    dockWidth = 700;
    const log: string[] = [];
    render(<Surface yields={[fakeYield("nav", 280, log), fakeYield("sidebar", 320, log)]} />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    expect(log).toEqual(["park nav", "park sidebar"]);
    expect(pane()).toBeInTheDocument();

    act(() => {
      fireEvent.keyDown(pane()!, { key: "Escape" });
    });
    expect(log).toEqual(["park nav", "park sidebar", "restore sidebar", "restore nav"]);
  });

  it("switches files in place without asking for room again", () => {
    dockWidth = 700;
    const log: string[] = [];
    render(<Surface yields={[fakeYield("nav", 280, log), fakeYield("sidebar", 320, log)]} />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    act(() => {
      fireEvent.click(screen.getByText("open third"));
    });

    expect(pane()).toHaveAccessibleName("Preview of shot-3.png");
    expect(screen.getByText("3 / 3")).toBeInTheDocument();
    expect(log).toEqual(["park nav", "park sidebar"]);
  });

  it("pages with the arrow keys while focus is in the pane", () => {
    dockWidth = 1300;
    render(<Surface />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    act(() => {
      fireEvent.keyDown(pane()!, { key: "ArrowRight" });
    });
    expect(pane()).toHaveAccessibleName("Preview of shot-3.png");
    expect(screen.getByTestId("active")).toHaveTextContent(THREE[2]!.id);
  });

  it("goes full screen over the pane, and Escape there returns to the pane", async () => {
    dockWidth = 1300;
    render(<Surface />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    act(() => {
      fireEvent.click(screen.getByRole("button", { name: "Full screen" }));
    });
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-label", "shot-2.png");

    act(() => {
      fireEvent.keyDown(document, { key: "Escape" });
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(pane()).toHaveAccessibleName("Preview of shot-2.png");
  });

  it("closes when the surface moves on to something else", async () => {
    dockWidth = 1300;
    const log: string[] = [];
    const nav = fakeYield("nav", 280, log);
    function Switcher() {
      const [scope, setScope] = useState("issue-1");
      return (
        <>
          <button type="button" onClick={() => setScope("issue-2")}>
            next issue
          </button>
          <Surface yields={[nav]} scopeKey={scope} />
        </>
      );
    }
    dockWidth = 1000;
    render(<Switcher />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    expect(pane()).toBeInTheDocument();

    act(() => {
      fireEvent.click(screen.getByText("next issue"));
    });
    await waitFor(() => expect(pane()).not.toBeInTheDocument());
    expect(log).toEqual(["park nav", "restore nav"]);
  });

  it("remembers the width the reader gives the pane", () => {
    dockWidth = 1400;
    render(<Surface />);
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    const separator = screen.getByRole("separator", { name: "Resize preview" });
    expect(separator).toHaveAttribute("aria-valuenow", String(PREVIEW_PANE_DEFAULT_WIDTH));

    act(() => {
      fireEvent.keyDown(separator, { key: "ArrowLeft" });
    });
    expect(separator).toHaveAttribute("aria-valuenow", String(PREVIEW_PANE_DEFAULT_WIDTH + 16));
    expect(localStorage.getItem("multica_preview_pane_width")).toBe(String(PREVIEW_PANE_DEFAULT_WIDTH + 16));
  });
});
