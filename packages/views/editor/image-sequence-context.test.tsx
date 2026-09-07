import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  fireEvent,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactElement } from "react";
import type { Attachment } from "@multica/core/types";
import { collectImageSequence } from "@multica/core/attachments/image-sequence";

const { downloadMock, getBaseUrlMock, toastErrorMock } = vi.hoisted(() => ({
  downloadMock: vi.fn(),
  getBaseUrlMock: vi.fn(() => ""),
  toastErrorMock: vi.fn(),
}));

vi.mock("../platform", () => ({ openExternal: vi.fn() }));

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: getBaseUrlMock, getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("./use-download-attachment", () => ({
  useDownloadAttachment: () => downloadMock,
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

vi.mock("./readonly-content", () => ({
  ReadonlyContent: () => null,
}));

vi.mock("sonner", () => ({ toast: { error: toastErrorMock } }));

const STRINGS: Record<string, Record<string, string>> = {
  image: {
    download: "Download",
    canvas_label: "Image canvas",
    previous: "Previous image",
    next: "Next image",
    sequence_position: "{{index}} / {{total}}",
    unavailable: "That image is no longer available — skipped it.",
  },
  canvas: {
    zoom_in: "Zoom in",
    zoom_out: "Zoom out",
    zoom_fit: "Fit to view",
    zoom_actual: "Actual size",
  },
  attachment: {
    close: "Close",
    preview_unsupported: "This file type can't be previewed.",
    open_in_new_tab: "Open in new tab",
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
  ImageSequenceProvider,
  useImageSequencePreview,
} from "./image-sequence-context";

function render(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
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

// Each attachment lands in its own block, mirroring three comments each
// carrying one screenshot.
function sequenceOf(attachments: Attachment[]) {
  return collectImageSequence(attachments.map((a) => ({ attachments: [a] })));
}

function Opener({ openKey }: { openKey: string }) {
  const sequence = useImageSequencePreview();
  return (
    <button type="button" onClick={() => sequence.openAt(openKey)}>
      open
    </button>
  );
}

// The "X / Y" readout is the only place the modal prints a bare count.
function expectCounter(text: string) {
  expect(screen.getByText(text)).toBeInTheDocument();
}

function prevButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "Previous image" });
}
function nextButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "Next image" });
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("ImageSequenceProvider", () => {
  it("opens at the clicked image's real position and reports X / Y", () => {
    render(
      <ImageSequenceProvider items={sequenceOf(THREE)}>
        <Opener openKey={THREE[1]!.id} />
      </ImageSequenceProvider>,
    );

    act(() => {
      fireEvent.click(screen.getByText("open"));
    });

    expectCounter("2 / 3");
    expect(screen.getByRole("dialog")).toHaveAttribute(
      "aria-label",
      "shot-2.png",
    );
  });

  it("walks forward and back without wrapping, disabling at each end", () => {
    render(
      <ImageSequenceProvider items={sequenceOf(THREE)}>
        <Opener openKey={THREE[0]!.id} />
      </ImageSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });

    // First image: no previous.
    expectCounter("1 / 3");
    expect(prevButton()).toBeDisabled();
    expect(nextButton()).not.toBeDisabled();

    act(() => {
      fireEvent.click(nextButton());
    });
    expectCounter("2 / 3");
    expect(prevButton()).not.toBeDisabled();

    act(() => {
      fireEvent.click(nextButton());
    });
    // Last image: no next, and clicking it cannot wrap to the first.
    expectCounter("3 / 3");
    expect(nextButton()).toBeDisabled();

    act(() => {
      fireEvent.click(prevButton());
    });
    expectCounter("2 / 3");
  });

  it("moves with the left / right arrow keys", () => {
    render(
      <ImageSequenceProvider items={sequenceOf(THREE)}>
        <Opener openKey={THREE[0]!.id} />
      </ImageSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });

    act(() => {
      fireEvent.keyDown(document, { key: "ArrowRight" });
    });
    expectCounter("2 / 3");

    act(() => {
      fireEvent.keyDown(document, { key: "ArrowLeft" });
    });
    expectCounter("1 / 3");

    // At the first image ArrowLeft is inert rather than wrapping.
    act(() => {
      fireEvent.keyDown(document, { key: "ArrowLeft" });
    });
    expectCounter("1 / 3");
  });

  it("freezes the sequence at open time so later images can't shift the index", () => {
    function Harness() {
      const [items, setItems] = useState(sequenceOf(THREE));
      return (
        <ImageSequenceProvider items={items}>
          <Opener openKey={THREE[2]!.id} />
          <button
            type="button"
            onClick={() =>
              setItems(sequenceOf([imageAttachment(9), ...THREE]))
            }
          >
            grow
          </button>
        </ImageSequenceProvider>
      );
    }
    render(<Harness />);

    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    expectCounter("3 / 3");

    // A comment lands while the preview is open.
    act(() => {
      fireEvent.click(screen.getByText("grow"));
    });
    expectCounter("3 / 3");
  });

  it("skips a broken image and says so", () => {
    render(
      <ImageSequenceProvider items={sequenceOf(THREE)}>
        <Opener openKey={THREE[0]!.id} />
      </ImageSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    act(() => {
      fireEvent.click(nextButton());
    });
    expectCounter("2 / 3");

    act(() => {
      fireEvent.error(screen.getByRole("dialog").querySelector("img")!);
    });

    // Kept moving the way the reader was going, and the dead frame is now out
    // of the walk in both directions.
    expectCounter("3 / 3");
    expect(toastErrorMock).toHaveBeenCalledTimes(1);
    act(() => {
      fireEvent.click(prevButton());
    });
    expectCounter("1 / 3");
  });

  it("reports false for an image the surface does not know", () => {
    const seen: boolean[] = [];
    function Probe() {
      const sequence = useImageSequencePreview();
      return (
        <button
          type="button"
          onClick={() => seen.push(sequence.openAt("https://cdn/unknown.png"))}
        >
          try
        </button>
      );
    }
    render(
      <ImageSequenceProvider items={sequenceOf(THREE)}>
        <Probe />
      </ImageSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("try"));
    });
    expect(seen).toEqual([false]);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("leaves a lone image with no sequence chrome", () => {
    render(
      <ImageSequenceProvider items={sequenceOf([THREE[0]!])}>
        <Opener openKey={THREE[0]!.id} />
      </ImageSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next image" })).toBeNull();
  });
});

describe("useImageSequencePreview without a provider", () => {
  it("reports false so the caller can fall back to a single preview", () => {
    const seen: boolean[] = [];
    function Probe() {
      const sequence = useImageSequencePreview();
      return (
        <button type="button" onClick={() => seen.push(sequence.openAt("k"))}>
          try
        </button>
      );
    }
    render(<Probe />);
    act(() => {
      fireEvent.click(screen.getByText("try"));
    });
    expect(seen).toEqual([false]);
  });
});
