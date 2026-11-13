import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { MetadataSection } from "./metadata-section";

vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const metadata = { pipeline_status: "waiting_review", pr_number: 61, passed: false, note: "长文本\n第二行" };

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(copyText).mockResolvedValue(true);
});

describe("MetadataSection", () => {
  it("hides empty metadata", () => {
    const { container } = renderWithI18n(<MetadataSection metadata={{}} />);
    expect(container).toBeEmptyDOMElement();
  });
  it("uses the shared count and expands values inline without a dialog", () => {
    renderWithI18n(<MetadataSection metadata={metadata} />);
    const header = screen.getByRole("button", { name: "Metadata 4" });
    expect(header).toHaveAttribute("aria-expanded", "false");
    expect(within(header).getByText("4")).toHaveAttribute("data-slot", "section-count");
    expect(screen.queryByText("pipeline_status")).not.toBeInTheDocument();
    fireEvent.click(header);
    expect(screen.getByText("waiting_review")).toBeVisible();
    expect(screen.getByText("61")).toBeVisible();
    expect(screen.getByText("false")).toBeVisible();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(header);
    expect(screen.queryByText("waiting_review")).not.toBeInTheDocument();
  });
  it("copies complete formatted JSON even while collapsed and reports failure", async () => {
    renderWithI18n(<MetadataSection metadata={metadata} />);
    fireEvent.click(screen.getByRole("button", { name: "Copy metadata" }));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Copied"));
    expect(copyText).toHaveBeenCalledWith(JSON.stringify(metadata, null, 2));
    vi.mocked(copyText).mockResolvedValue(false);
    fireEvent.click(screen.getByRole("button", { name: "Copy metadata" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Failed to copy metadata"));
    expect(toast.success).toHaveBeenCalledTimes(1);
  });
  it("opens the enlarged view, copies, resizes and returns focus after Escape", async () => {
    const user = userEvent.setup();
    renderWithI18n(<MetadataSection metadata={metadata} />);
    const trigger = screen.getByRole("button", { name: "View metadata" });
    await user.click(trigger);
    const dialog = await screen.findByRole("dialog", { name: "Metadata" });
    expect(within(dialog).getByText("waiting_review")).toBeVisible();
    await user.click(within(dialog).getByRole("button", { name: "Copy metadata" }));
    expect(copyText).toHaveBeenCalledWith(JSON.stringify(metadata, null, 2));
    await user.click(within(dialog).getByRole("button", { name: "Expand dialog" }));
    expect(within(dialog).getByRole("button", { name: "Restore dialog size" })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(trigger).toHaveFocus();
    await user.click(trigger);
    expect(await screen.findByRole("button", { name: "Expand dialog" })).toBeVisible();
  });
});
