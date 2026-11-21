// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { TextAvatarDialog } from "./text-avatar-dialog";

const render = vi.hoisted(() => vi.fn());
vi.mock("./text-avatar", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./text-avatar")>()),
  renderTextAvatar: render,
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

function mount(name = "acme") {
  const props = {
    open: true,
    name,
    busy: false,
    onOpenChange: vi.fn(),
    onGenerated: vi.fn(),
  };
  renderWithI18n(<TextAvatarDialog {...props} />);
  const input = screen.getByLabelText("Text") as HTMLInputElement;
  const save = screen.getByRole("button", { name: "Save" });
  return { props, input, save };
}

describe("TextAvatarDialog", () => {
  it("starts from the name's first character", () => {
    const { input, save } = mount("acme-a");
    expect(input.value).toBe("A");
    expect(save).toBeEnabled();
  });

  it("cannot save empty text", () => {
    const { input, save } = mount();
    fireEvent.change(input, { target: { value: "   " } });

    expect(save).toBeDisabled();
    fireEvent.click(save);
    fireEvent.keyDown(input, { key: "Enter" });
    expect(render).not.toHaveBeenCalled();
  });

  it("cuts typed text to two characters", () => {
    const { input } = mount();
    fireEvent.change(input, { target: { value: "ABC" } });
    expect(input.value).toBe("AB");
  });

  it("waits for an IME composition to finish before cutting", () => {
    const { input } = mount();
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "yanfa" } });
    expect(input.value).toBe("yanfa");

    fireEvent.change(input, { target: { value: "研发部" } });
    fireEvent.compositionEnd(input);
    expect(input.value).toBe("研发");
  });

  it("hands the chosen text and color to the renderer, then the file to the parent", async () => {
    const png = new File(["png"], "workspace-icon.png", { type: "image/png" });
    render.mockResolvedValueOnce(png);
    const { input, save, props } = mount();

    fireEvent.change(input, { target: { value: "A4" } });
    fireEvent.click(screen.getByRole("button", { name: "#22c55e" }));
    expect(screen.getByRole("button", { name: "#22c55e" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.click(save);

    await waitFor(() => expect(props.onGenerated).toHaveBeenCalledWith(png));
    expect(render).toHaveBeenCalledWith("A4", "#22c55e");
  });

  it("shows an error and stays open when drawing fails", async () => {
    render.mockRejectedValueOnce(new Error("no canvas"));
    const { save, props } = mount();

    fireEvent.click(save);

    expect(
      await screen.findByText("Couldn't create the icon. Please try again."),
    ).toBeInTheDocument();
    expect(props.onGenerated).not.toHaveBeenCalled();
    expect(props.onOpenChange).not.toHaveBeenCalled();
  });

  it("locks while the parent is uploading", () => {
    renderWithI18n(
      <TextAvatarDialog
        open
        name="acme"
        busy
        onOpenChange={vi.fn()}
        onGenerated={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: /Uploading/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByLabelText("Text")).toBeDisabled();
  });
});
