import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enEditor from "../locales/en/editor.json";

// The HTML body is fetched via this hook; stub it so the page renders the iframe
// branch without a network/query-client.
vi.mock("../editor/hooks/use-attachment-html-text", () => ({
  useAttachmentHtmlText: () => ({
    data: { text: "<p>searchable body</p>" },
    isLoading: false,
    error: null,
  }),
}));

import { AttachmentPreviewPage } from "./attachment-preview-page";

const resources = { en: { editor: enEditor } };

function renderPage() {
  return render(
    <I18nProvider locale="en" resources={resources}>
      <AttachmentPreviewPage attachmentId="att-1" filename="report.html" />
    </I18nProvider>,
  );
}

describe("AttachmentPreviewPage — in-page find (#5259)", () => {
  it("does not show the find bar until requested", () => {
    renderPage();
    expect(screen.queryByPlaceholderText("Find in page")).not.toBeInTheDocument();
  });

  it("opens the find bar on Ctrl+F and closes it on Escape", () => {
    renderPage();

    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    expect(screen.getByPlaceholderText("Find in page")).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByPlaceholderText("Find in page")).not.toBeInTheDocument();
  });

  it("opens the find bar on Cmd+F (macOS)", () => {
    renderPage();
    fireEvent.keyDown(window, { key: "f", metaKey: true });
    expect(screen.getByPlaceholderText("Find in page")).toBeInTheDocument();
  });

  it("renders the sandboxed iframe with the injected find shim in srcDoc", () => {
    const { container } = renderPage();
    const iframe = container.querySelector("iframe");
    expect(iframe).toBeTruthy();
    // Sandbox posture preserved: allow-scripts, never allow-same-origin.
    expect(iframe!.getAttribute("sandbox")).toBe("allow-scripts");
    expect(iframe!.getAttribute("srcdoc")).toContain("multica-find-cmd");
  });
});
