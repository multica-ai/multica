import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { configStore } from "@multica/core/config";
import { renderWithI18n } from "../test/i18n";

const prepareDrawioEmbed = vi.hoisted(() => vi.fn());
const forgetDrawioViewerProbe = vi.hoisted(() => vi.fn());
vi.mock("./utils/drawio", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./utils/drawio")>()),
  prepareDrawioEmbed,
  forgetDrawioViewerProbe,
}));

import { DrawioBlock, DrawioStage } from "./drawio-preview";
import { RichFenceBlock } from "../rich-content/rich-code-block";

const VIEWER = "https://viewer.example.test";
const XML = '<mxfile><diagram name="Page-1"><mxGraphModel><root/></mxGraphModel></diagram></mxfile>';
const SRC = `${VIEWER}/?lightbox=1&nav=1#Rabc`;

beforeEach(() => {
  prepareDrawioEmbed.mockResolvedValue({ ok: true, src: SRC });
});

afterEach(() => {
  configStore.setState({ drawioViewerUrl: "" });
  vi.clearAllMocks();
});

describe("DrawioBlock", () => {
  it("embeds the viewer in an isolated frame that sends no referrer", async () => {
    renderWithI18n(<DrawioBlock xml={XML} title="Architecture" viewerUrl={VIEWER} />);

    const frame = await screen.findByTitle("Architecture");
    expect(frame.getAttribute("src")).toBe(SRC);
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-same-origin");
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");
    expect(prepareDrawioEmbed).toHaveBeenCalledWith(XML, VIEWER);
  });

  it.each([
    ["invalid", "This isn't valid draw.io XML."],
    ["too_large", "This diagram is too large to preview here."],
    ["unreachable", `Couldn't reach the draw.io viewer at ${VIEWER}.`],
  ])("explains a %s diagram instead of drawing an empty frame", async (error, message) => {
    prepareDrawioEmbed.mockResolvedValue({ ok: false, error });
    renderWithI18n(<DrawioBlock xml={XML} viewerUrl={VIEWER} />);

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Couldn't show this draw.io diagram");
    expect(alert.textContent).toContain(message);
    expect(document.querySelector("iframe")).toBeNull();
  });

  it("probes the viewer again on reload", async () => {
    prepareDrawioEmbed.mockResolvedValueOnce({ ok: false, error: "unreachable" });
    renderWithI18n(<DrawioBlock xml={XML} viewerUrl={VIEWER} />);
    await screen.findByRole("alert");

    fireEvent.click(screen.getByRole("button", { name: "Reload" }));

    expect(forgetDrawioViewerProbe).toHaveBeenCalledWith(VIEWER);
    expect(await screen.findByTitle("draw.io diagram")).toBeTruthy();
    expect(prepareDrawioEmbed).toHaveBeenCalledTimes(2);
  });
});

describe("DrawioStage", () => {
  it("hands errors to the viewer's fallback", async () => {
    prepareDrawioEmbed.mockResolvedValue({ ok: false, error: "too_large" });
    renderWithI18n(
      <DrawioStage
        xml={XML}
        title="arch.drawio"
        viewerUrl={VIEWER}
        renderError={(message) => <p data-testid="fallback">{message}</p>}
      />,
    );
    expect((await screen.findByTestId("fallback")).textContent).toBe(
      "This diagram is too large to preview here.",
    );
  });
});

describe("RichFenceBlock ```drawio", () => {
  it("shows the XML when the deployment has no viewer", () => {
    configStore.setState({ drawioViewerUrl: "" });
    renderWithI18n(<RichFenceBlock language="drawio" body={XML} />);

    expect(document.querySelector("iframe")).toBeNull();
    expect(document.querySelector("[data-dynamic-block]")).toBeNull();
    expect(document.querySelector("pre")?.textContent).toContain("<mxfile>");
    expect(prepareDrawioEmbed).not.toHaveBeenCalled();
  });

  it("refuses a viewer on the app's own origin", () => {
    configStore.setState({ drawioViewerUrl: `${window.location.origin}/drawio` });
    renderWithI18n(<RichFenceBlock language="drawio" body={XML} />);

    expect(document.querySelector("[data-dynamic-block]")).toBeNull();
    expect(document.querySelector("pre")?.textContent).toContain("<mxfile>");
  });

  it("upgrades to the diagram block when a viewer is configured", async () => {
    configStore.setState({ drawioViewerUrl: VIEWER });
    renderWithI18n(<RichFenceBlock language="drawio" body={XML} />);

    await waitFor(() => {
      expect(document.querySelector('[data-dynamic-block="drawio"]')).not.toBeNull();
    });
  });
});
