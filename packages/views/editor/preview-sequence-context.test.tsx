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
    view: "View",
    copy_link: "Copy link",
    canvas_label: "Image canvas",
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
    previous: "Previous",
    next: "Next",
    sequence_position: "{{index}} / {{total}}",
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
  usePreviewSequence,
} from "./preview-sequence-context";
import { Attachment as InlineAttachment } from "./attachment";
import { AttachmentDownloadProvider } from "./attachment-download-context";

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
// carrying one file.
function sequenceOf(attachments: Attachment[]) {
  return collectPreviewSequence(attachments.map((a) => ({ attachments: [a] })));
}

// jsdom has no Image.decode(); the viewer swaps images without one, so the
// decode-then-swap path only runs under a stub. Returns the restore.
function stubDecode(impl: (this: HTMLImageElement) => Promise<void>): () => void {
  Object.defineProperty(HTMLImageElement.prototype, "decode", {
    configurable: true,
    writable: true,
    value: impl,
  });
  return () => {
    delete (HTMLImageElement.prototype as { decode?: unknown }).decode;
  };
}

function fileAttachment(n: number, filename: string, contentType: string): Attachment {
  return { ...imageAttachment(n), filename, content_type: contentType };
}

function Opener({ openKey }: { openKey: string }) {
  const sequence = usePreviewSequence();
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
  return screen.getByRole("button", { name: "Previous" });
}
function nextButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "Next" });
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("PreviewSequenceProvider", () => {
  it("opens at the clicked image's real position and reports X / Y", () => {
    render(
      <PreviewSequenceProvider items={sequenceOf(THREE)}>
        <Opener openKey={THREE[1]!.id} />
      </PreviewSequenceProvider>,
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
      <PreviewSequenceProvider items={sequenceOf(THREE)}>
        <Opener openKey={THREE[0]!.id} />
      </PreviewSequenceProvider>,
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
      <PreviewSequenceProvider items={sequenceOf(THREE)}>
        <Opener openKey={THREE[0]!.id} />
      </PreviewSequenceProvider>,
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
        <PreviewSequenceProvider items={items}>
          <Opener openKey={THREE[2]!.id} />
          <button
            type="button"
            onClick={() =>
              setItems(sequenceOf([imageAttachment(9), ...THREE]))
            }
          >
            grow
          </button>
        </PreviewSequenceProvider>
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
      <PreviewSequenceProvider items={sequenceOf(THREE)}>
        <Opener openKey={THREE[0]!.id} />
      </PreviewSequenceProvider>,
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

  it("pages onto files that are not images", () => {
    const mixed = [
      imageAttachment(1),
      fileAttachment(2, "spec.pdf", "application/pdf"),
      imageAttachment(3),
    ];
    render(
      <PreviewSequenceProvider items={sequenceOf(mixed)}>
        <Opener openKey={mixed[0]!.id} />
      </PreviewSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });

    act(() => {
      fireEvent.click(nextButton());
    });
    expectCounter("2 / 3");
    expect(screen.getByTitle("spec.pdf").tagName).toBe("IFRAME");

    act(() => {
      fireEvent.click(nextButton());
    });
    expectCounter("3 / 3");
    expect(screen.getByRole("dialog").querySelector("img")).not.toBeNull();
  });

  it("leaves the arrow keys to a focused video", () => {
    const mixed = [
      fileAttachment(1, "demo.mp4", "video/mp4"),
      imageAttachment(2),
    ];
    render(
      <PreviewSequenceProvider items={sequenceOf(mixed)}>
        <Opener openKey={mixed[0]!.id} />
      </PreviewSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });

    const video = screen.getByRole("dialog").querySelector("video")!;
    act(() => {
      fireEvent.keyDown(video, { key: "ArrowRight" });
    });
    // The player seeks; the viewer stays put.
    expectCounter("1 / 2");

    act(() => {
      fireEvent.keyDown(document, { key: "ArrowRight" });
    });
    expectCounter("2 / 2");
  });

  // Review of #8763: the panel is reused across kinds, so the frame it held
  // for a PDF must never reach the image canvas while the next image decodes.
  it("shows the image itself, not the previous file, while it decodes", () => {
    const restoreDecode = stubDecode(() => new Promise<void>(() => {}));
    try {
      const mixed = [
        fileAttachment(1, "spec.pdf", "application/pdf"),
        imageAttachment(2),
        imageAttachment(3),
      ];
      render(
        <PreviewSequenceProvider items={sequenceOf(mixed)}>
          <Opener openKey={mixed[0]!.id} />
        </PreviewSequenceProvider>,
      );
      act(() => {
        fireEvent.click(screen.getByText("open"));
      });
      act(() => {
        fireEvent.click(nextButton());
      });

      const image = screen.getByRole("dialog").querySelector("img")!;
      expect(image.getAttribute("src")).toBe(mixed[1]!.download_url);
      expectCounter("2 / 3");
      expect(nextButton()).not.toBeDisabled();
      expect(toastErrorMock).not.toHaveBeenCalled();
    } finally {
      restoreDecode();
    }
  });

  it("does not blame the next image for an error on the frame still held", async () => {
    const [first, second] = [imageAttachment(1), imageAttachment(2)];
    const restoreDecode = stubDecode(function (this: HTMLImageElement) {
      return this.src === first.download_url
        ? Promise.resolve()
        : new Promise<void>(() => {});
    });
    try {
      render(
        <PreviewSequenceProvider items={sequenceOf([first, second])}>
          <Opener openKey={first.id} />
        </PreviewSequenceProvider>,
      );
      act(() => {
        fireEvent.click(screen.getByText("open"));
      });
      await act(async () => {});
      act(() => {
        fireEvent.click(nextButton());
      });

      // The first image stays up while the second decodes; an error from it
      // is about the first file, not the one being opened.
      const held = screen.getByRole("dialog").querySelector("img")!;
      expect(held.getAttribute("src")).toBe(first.download_url);
      act(() => {
        fireEvent.error(held);
      });
      expectCounter("2 / 2");
      expect(toastErrorMock).not.toHaveBeenCalled();
    } finally {
      restoreDecode();
    }
  });

  it("reports false for an image the surface does not know", () => {
    const seen: boolean[] = [];
    function Probe() {
      const sequence = usePreviewSequence();
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
      <PreviewSequenceProvider items={sequenceOf(THREE)}>
        <Probe />
      </PreviewSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("try"));
    });
    expect(seen).toEqual([false]);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("leaves a lone image with no sequence chrome", () => {
    render(
      <PreviewSequenceProvider items={sequenceOf([THREE[0]!])}>
        <Opener openKey={THREE[0]!.id} />
      </PreviewSequenceProvider>,
    );
    act(() => {
      fireEvent.click(screen.getByText("open"));
    });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next" })).toBeNull();
  });
});

// An agent that rewrites an issue body swaps the markdown image URL but does
// not register the new file as an attachment of that issue, so these images
// reach the viewer with no server metadata at all — the caption is the only
// "filename" there is. It is a caption, not a file name (MUL-7518).
describe("body images with no attachment record", () => {
  function CaptionedBody({ captions }: { captions: string[] }) {
    const urls = captions.map((_, i) => `https://cdn.example.test/chart-${i}.png`);
    const content = captions
      .map((caption, i) => `![${caption}](${urls[i]})`)
      .join("\n\n");
    return (
      <AttachmentDownloadProvider attachments={[]}>
        <PreviewSequenceProvider
          items={collectPreviewSequence([{ content, attachments: [] }])}
        >
          {captions.map((caption, i) => (
            <InlineAttachment
              key={urls[i]}
              attachment={{
                kind: "url",
                url: urls[i]!,
                filename: caption,
                forceKind: "image",
              }}
            />
          ))}
        </PreviewSequenceProvider>
      </AttachmentDownloadProvider>
    );
  }

  it.each(["报告图表", ""])(
    "zooms a body image whose caption is %j, not a filename",
    (caption) => {
      render(<CaptionedBody captions={[caption]} />);

      act(() => {
        fireEvent.click(screen.getAllByTitle("View")[0]!);
      });

      expect(
        screen.queryByText("This file type can't be previewed."),
      ).toBeNull();
      expect(screen.getByRole("dialog").querySelector("img")).not.toBeNull();
    },
  );

  it("still pages between captioned body images", () => {
    render(<CaptionedBody captions={["报告图表", "对比图"]} />);

    act(() => {
      fireEvent.click(screen.getAllByTitle("View")[0]!);
    });
    expectCounter("1 / 2");

    act(() => {
      fireEvent.click(nextButton());
    });
    expectCounter("2 / 2");
    expect(screen.getByRole("dialog").querySelector("img")).not.toBeNull();
  });
});

describe("usePreviewSequence without a provider", () => {
  it("reports false so the caller can fall back to a single preview", () => {
    const seen: boolean[] = [];
    function Probe() {
      const sequence = usePreviewSequence();
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
