// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://api.test" },
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ upload: vi.fn() }),
}));

// The crop dialog only matters after a file is picked; nothing here picks one.
vi.mock("./avatar-crop-dialog", () => ({
  AvatarCropDialog: () => null,
}));

import { AvatarUploadControl } from "./avatar-upload-control";

afterEach(cleanup);

function openPicker() {
  fireEvent.click(screen.getByRole("button", { name: "Change avatar" }));
}

function spyOnFileDialog(container: HTMLElement) {
  const input = container.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("file input not rendered");
  return vi.spyOn(input, "click").mockImplementation(() => {});
}

describe("AvatarUploadControl", () => {
  // Without an emoji handler the control keeps its original single-purpose
  // shape: one click, one file dialog, no menu in between.
  it("goes straight to the file dialog when emoji is not offered", () => {
    const { container } = renderWithI18n(
      <AvatarUploadControl variant="agent" value={null} onUploaded={vi.fn()} />,
    );
    const openDialog = spyOnFileDialog(container);

    fireEvent.click(screen.getByRole("button", { name: "Change avatar" }));

    expect(openDialog).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Upload image")).not.toBeInTheDocument();
  });

  it("offers both an image upload and emoji suggestions", async () => {
    renderWithI18n(
      <AvatarUploadControl
        variant="agent"
        value={null}
        onUploaded={vi.fn()}
        onEmojiSelected={vi.fn()}
      />,
    );

    openPicker();

    expect(
      await screen.findByRole("button", { name: "Upload image" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "🦊" })).toBeInTheDocument();
  });

  // The caller persists whatever it gets straight into `avatar_url`, so the
  // control has to emit the marker the renderers parse, not the bare emoji.
  it("emits the emoji marker value the server stores", async () => {
    const onEmojiSelected = vi.fn();
    renderWithI18n(
      <AvatarUploadControl
        variant="agent"
        value={null}
        onUploaded={vi.fn()}
        onEmojiSelected={onEmojiSelected}
      />,
    );

    openPicker();
    fireEvent.click(await screen.findByRole("button", { name: "🦊" }));

    await waitFor(() =>
      expect(onEmojiSelected).toHaveBeenCalledWith("emoji:🦊"),
    );
  });

  it("still reaches the file dialog through the picker", async () => {
    const { container } = renderWithI18n(
      <AvatarUploadControl
        variant="agent"
        value={null}
        onUploaded={vi.fn()}
        onEmojiSelected={vi.fn()}
      />,
    );
    const openDialog = spyOnFileDialog(container);

    openPicker();
    fireEvent.click(await screen.findByRole("button", { name: "Upload image" }));

    expect(openDialog).toHaveBeenCalledTimes(1);
  });

  it("marks the agent's current emoji as the selected suggestion", async () => {
    renderWithI18n(
      <AvatarUploadControl
        variant="agent"
        value="emoji:🚀"
        onUploaded={vi.fn()}
        onEmojiSelected={vi.fn()}
      />,
    );

    openPicker();

    expect(await screen.findByRole("button", { name: "🚀" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "🦊" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("cannot be opened when the caller lacks edit permission", () => {
    renderWithI18n(
      <AvatarUploadControl
        variant="agent"
        value={null}
        disabled
        onUploaded={vi.fn()}
        onEmojiSelected={vi.fn()}
      />,
    );

    const trigger = screen.getByRole("button", { name: "Change avatar" });
    expect(trigger).toBeDisabled();
    fireEvent.click(trigger);
    expect(screen.queryByText("Upload image")).not.toBeInTheDocument();
  });
});
