import { describe, it, expect, beforeAll, vi } from "vitest";
import { screen, fireEvent } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { WebhookPayloadPreview } from "./webhook-payload-preview";

// sonner.toast is a fire-and-forget side-effect we don't want to assert on
// in these tests; stub it so the Copy button doesn't blow up on toast
// invocation.
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// jsdom doesn't provide navigator.clipboard by default. Stub it once.
beforeAll(() => {
  Object.assign(navigator, {
    clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
  });
});

const envelope = (event: string, eventPayload: unknown, extras: Record<string, unknown> = {}) => ({
  event,
  eventPayload,
  request: { receivedAt: "2026-05-13T12:34:56Z", contentType: "application/json", ...extras },
});

describe("WebhookPayloadPreview", () => {
  it("renders the envelope event in the header", () => {
    renderWithI18n(
      <WebhookPayloadPreview
        payload={envelope("github.pull_request.opened", { number: 1 })}
        defaultOpen
      />,
    );
    expect(screen.getByText("github.pull_request.opened")).toBeInTheDocument();
  });

  it("falls back gracefully when payload is not an envelope", () => {
    renderWithI18n(
      <WebhookPayloadPreview payload={{ hello: "world" }} defaultOpen />,
    );
    // The unknown-event placeholder is the i18n key; the body should still
    // include the raw JSON so nothing is hidden.
    expect(screen.getByText(/hello/)).toBeInTheDocument();
  });

  it("truncates display when the payload exceeds 4 KiB but copies full text", async () => {
    // 5 KiB string field → stringified envelope > 4 KiB.
    const bigPayload = envelope("demo.big", { blob: "x".repeat(5 * 1024) });
    renderWithI18n(
      <WebhookPayloadPreview payload={bigPayload} defaultOpen />,
    );
    // Truncation marker (i18n) appears as a tail span — we assert by
    // partial text rather than coupling to the exact phrasing.
    expect(screen.getByText(/truncated/i)).toBeInTheDocument();

    // The visible <pre> body must NOT contain the full 5 KiB blob — it is
    // sliced to the truncate threshold.
    const pre = document.querySelector("pre");
    expect(pre).not.toBeNull();
    expect((pre!.textContent ?? "").length).toBeLessThan(5 * 1024 + 200);

    // Clicking Copy must still hand the FULL payload to the clipboard.
    fireEvent.click(screen.getByRole("button", { name: /copy/i }));
    const writeText = navigator.clipboard.writeText as ReturnType<typeof vi.fn>;
    expect(writeText).toHaveBeenCalled();
    const lastCall = writeText.mock.calls[writeText.mock.calls.length - 1];
    if (!lastCall) throw new Error("clipboard.writeText was not called");
    const written = lastCall[0] as string;
    expect(written.length).toBeGreaterThan(5 * 1024);
    expect(written).toContain("xxxxxxxx");
  });
});
