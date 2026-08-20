import { describe, it, expect, beforeAll, vi } from "vitest";
import { screen, fireEvent } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { WebhookUrlField } from "./webhook-url-field";

// sonner.toast is a fire-and-forget side-effect; stub it so Copy doesn't blow
// up on toast invocation.
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const writeText = vi.fn().mockResolvedValue(undefined);

// jsdom doesn't provide navigator.clipboard by default. Stub it once.
beforeAll(() => {
  Object.assign(navigator, { clipboard: { writeText } });
});

const URL = "https://api.example.com/api/webhooks/autopilots/awt_supersecret";

describe("WebhookUrlField", () => {
  it("hides the token by default — the plaintext is not in the DOM", () => {
    const { container } = renderWithI18n(<WebhookUrlField url={URL} />);
    expect(container.textContent).not.toContain("awt_supersecret");
    expect(screen.getByText(/api\/webhooks\/autopilots\/•+/)).toBeInTheDocument();
  });

  it("reveals the full URL when the masked value is clicked", () => {
    const { container } = renderWithI18n(<WebhookUrlField url={URL} />);
    fireEvent.click(screen.getByRole("button", { name: "Webhook URL hidden — click to show" }));
    expect(container.textContent).toContain(URL);
  });

  it("hides again via the toggle", () => {
    const { container } = renderWithI18n(<WebhookUrlField url={URL} />);
    fireEvent.click(screen.getByRole("button", { name: "Webhook URL hidden — click to show" }));
    fireEvent.click(screen.getByRole("button", { name: "Hide URL" }));
    expect(container.textContent).not.toContain("awt_supersecret");
  });

  it("copies the real URL while it is still hidden", async () => {
    writeText.mockClear();
    const { container } = renderWithI18n(<WebhookUrlField url={URL} />);
    fireEvent.click(screen.getByRole("button", { name: "Copy URL" }));
    expect(writeText).toHaveBeenCalledWith(URL);
    expect(container.textContent).not.toContain("awt_supersecret");
  });

  it("re-hides when the URL changes — a rotated token is never inherited revealed", () => {
    const rotated = "https://api.example.com/api/webhooks/autopilots/awt_rotatedsecret";
    const { container, rerender } = renderWithI18n(<WebhookUrlField url={URL} />);
    fireEvent.click(screen.getByRole("button", { name: "Webhook URL hidden — click to show" }));
    expect(container.textContent).toContain(URL);

    // Rotate: the row stays mounted and only the prop changes. The new
    // credential must come back masked, with no revealed frame in between.
    rerender(<WebhookUrlField url={rotated} />);
    expect(container.textContent).not.toContain("awt_rotatedsecret");
    expect(
      screen.getByRole("button", { name: "Webhook URL hidden — click to show" }),
    ).toBeInTheDocument();

    // The new URL can still be revealed on its own.
    fireEvent.click(screen.getByRole("button", { name: "Webhook URL hidden — click to show" }));
    expect(container.textContent).toContain(rotated);
  });

  it("renders trailing actions alongside the field", () => {
    renderWithI18n(
      <WebhookUrlField url={URL} actions={<button type="button">Rotate</button>} />,
    );
    expect(screen.getByRole("button", { name: "Rotate" })).toBeInTheDocument();
  });
});
