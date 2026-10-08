// @vitest-environment jsdom

import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";

const uploadMock = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({
  toast: { error: toastError, success: toastSuccess },
}));

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://api.test" },
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ upload: uploadMock }),
}));

// Stands in for the crop UI so the upload path is reachable without a real
// canvas; the cropping itself is covered by avatar-crop.test.ts.
vi.mock("./avatar-crop-dialog", () => ({
  AvatarCropDialog: ({
    open,
    onCropped,
  }: {
    open: boolean;
    onCropped: (file: File) => void;
  }) =>
    open ? (
      <button type="button" onClick={() => onCropped(imageFile())}>
        crop
      </button>
    ) : null,
}));

import { AvatarUploadControl } from "./avatar-upload-control";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function imageFile() {
  return new File(["x"], "avatar.png", { type: "image/png" });
}

function openPicker() {
  fireEvent.click(screen.getByRole("button", { name: "Change avatar" }));
}

function fileInput(container: HTMLElement) {
  const input = container.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("file input not rendered");
  return input;
}

function spyOnFileDialog(container: HTMLElement) {
  return vi.spyOn(fileInput(container), "click").mockImplementation(() => {});
}

// Drives the whole image path: choose a file, then crop-and-save.
function uploadImage(container: HTMLElement) {
  fireEvent.change(fileInput(container), {
    target: { files: [imageFile()] },
  });
  fireEvent.click(screen.getByRole("button", { name: "crop" }));
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

  // An edit caller PATCHes on every pick. Two in flight at once are
  // last-one-to-arrive-wins on the server, so the user's newer choice can lose
  // to the older one and stick — the avatar has to be locked until the save
  // settles.
  it("locks the avatar until a pick has finished saving", async () => {
    let settle = () => {};
    const onEmojiSelected = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          settle = resolve;
        }),
    );
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

    const trigger = screen.getByRole("button", { name: "Change avatar" });
    await waitFor(() => expect(trigger).toBeDisabled());

    // A second pick must not even be reachable while the first is in flight.
    fireEvent.click(trigger);
    expect(screen.queryByText("Upload image")).not.toBeInTheDocument();
    expect(onEmojiSelected).toHaveBeenCalledTimes(1);

    await act(async () => settle());
    await waitFor(() => expect(trigger).not.toBeDisabled());
  });

  // The caller that performed the write reports its own failure and rethrows so
  // autosave can show a failed state; toasting here too would double it up.
  it("leaves a failed save to the caller that performed it", async () => {
    renderWithI18n(
      <AvatarUploadControl
        variant="agent"
        value={null}
        onUploaded={vi.fn()}
        onEmojiSelected={() => Promise.reject(new Error("nope"))}
      />,
    );

    openPicker();
    fireEvent.click(await screen.findByRole("button", { name: "🦊" }));

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Change avatar" })).not
        .toBeDisabled(),
    );
    expect(toastError).not.toHaveBeenCalled();
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("leaves a failed image save to the caller too", async () => {
    uploadMock.mockResolvedValueOnce({ link: "https://cdn.test/a.png" });
    const { container } = renderWithI18n(
      <AvatarUploadControl
        variant="agent"
        value={null}
        onUploaded={() => Promise.reject(new Error("nope"))}
      />,
    );

    uploadImage(container);

    await waitFor(() => expect(uploadMock).toHaveBeenCalled());
    await waitFor(() => expect(toastSuccess).not.toHaveBeenCalled());
    expect(toastError).not.toHaveBeenCalled();
  });

  // The upload is the one failure this control owns — nothing else reports it.
  it("reports an upload failure itself", async () => {
    uploadMock.mockRejectedValueOnce(new Error("network down"));
    const { container } = renderWithI18n(
      <AvatarUploadControl variant="agent" value={null} onUploaded={vi.fn()} />,
    );

    uploadImage(container);

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("network down"));
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
