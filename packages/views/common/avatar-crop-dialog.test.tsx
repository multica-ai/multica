// @vitest-environment jsdom

import { useEffect } from "react";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { AvatarCropDialog } from "./avatar-crop-dialog";

const encode = vi.hoisted(() => vi.fn());
vi.mock("./avatar-crop", async (importOriginal) => ({
  ...await importOriginal<typeof import("./avatar-crop")>(),
  getCroppedAvatarBlob: encode,
  pickOutputType: () => ({ type: "image/webp", quality: 0.9 }),
}));
// jsdom has no image layout; the browser regression exercises the real cropper.
vi.mock("react-easy-crop", () => ({
  default: function LoadedCropper({ onCropComplete }: {
    onCropComplete: (area: object, pixels: object) => void;
  }) {
    useEffect(() => {
      onCropComplete({}, { x: 10, y: 20, width: 100, height: 100 });
      // A loaded image reports its initial crop once.
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    return null;
  },
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.stubGlobal("URL", class extends URL {
    static createObjectURL = vi.fn(() => "blob:synthetic-avatar");
    static revokeObjectURL = vi.fn();
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.resetAllMocks();
});

async function mount() {
  const pending = deferred<Blob>();
  encode.mockReturnValue(pending.promise);
  const props = {
    file: new File(["synthetic"], "avatar.png", { type: "image/png" }),
    open: true,
    busy: false,
    onOpenChange: vi.fn(),
    onCropped: vi.fn(),
  };
  const view = renderWithI18n(<AvatarCropDialog {...props} />);
  const apply = await screen.findByRole("button", { name: "Save" });
  await waitFor(() => expect(apply).toBeEnabled());
  return { ...view, props, pending, apply };
}

describe("AvatarCropDialog encoding", () => {
  it("locks controls and dismissal from Apply until the upload handoff", async () => {
    const { apply, pending, props } = await mount();
    fireEvent.click(apply);

    for (const name of ["Save", "Cancel", "Reset", "Rotate", "Zoom in", "Zoom out"]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
    }
    expect(screen.queryByRole("button", { name: "Close" })).not.toBeInTheDocument();
    expect(screen.getByRole("slider", { name: "Zoom" })).toBeDisabled();
    expect(apply).toHaveAttribute("aria-busy", "true");
    expect(screen.queryByText("Uploading…")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    const overlay = document.querySelector('[data-slot="dialog-overlay"]')!;
    fireEvent.mouseDown(overlay);
    fireEvent.mouseUp(overlay);
    fireEvent.click(overlay);
    expect(props.onOpenChange).not.toHaveBeenCalled();
    expect(props.onCropped).not.toHaveBeenCalled();

    await act(async () => pending.resolve(new Blob(["cropped"], { type: "image/webp" })));
    expect(props.onCropped).toHaveBeenCalledTimes(1);
    const file = props.onCropped.mock.calls[0]![0] as File;
    expect([file.name, file.type, file.size]).toEqual(["avatar.webp", "image/webp", 7]);
    expect(apply).toBeEnabled();
  });

  it("accepts only one encode for two clicks in the same event batch", async () => {
    const { apply, pending, props } = await mount();
    const cancel = screen.getByRole("button", { name: "Cancel" });
    const close = screen.getByRole("button", { name: "Close" });
    act(() => {
      apply.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      apply.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      cancel.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      close.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(encode).toHaveBeenCalledTimes(1);
    expect(props.onOpenChange).not.toHaveBeenCalled();
    await act(async () => pending.resolve(new Blob(["cropped"])));
    expect(props.onCropped).toHaveBeenCalledTimes(1);
  });

  it("releases the encoding lock on failure and keeps close available", async () => {
    const { apply, pending, props } = await mount();
    fireEvent.click(apply);
    await act(async () => pending.reject(new Error("synthetic encoding failure")));
    expect(screen.getByText("Couldn't load image")).toBeInTheDocument();
    expect(props.onCropped).not.toHaveBeenCalled();
    const cancel = screen.getByRole("button", { name: "Cancel" });
    expect(cancel).toBeEnabled();
    expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
    expect(apply).not.toHaveAttribute("aria-busy", "true");
    fireEvent.click(cancel);
    expect(props.onOpenChange).toHaveBeenCalledWith(false);
  });

  it("preserves the parent's busy lock after encoding settles", async () => {
    const { apply, pending, props, rerender } = await mount();
    fireEvent.click(apply);
    rerender(<AvatarCropDialog {...props} busy />);
    await act(async () => pending.resolve(new Blob(["cropped"])));
    expect(screen.getByRole("button", { name: /Uploading/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(props.onOpenChange).not.toHaveBeenCalled();
    rerender(<AvatarCropDialog {...props} />);
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("allows cancellation before Apply without starting an encode", async () => {
    const { props } = await mount();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(props.onOpenChange).toHaveBeenCalledWith(false);
    expect(encode).not.toHaveBeenCalled();
  });

  it("keeps the selected rotation and crop in the encoder request", async () => {
    const { apply, pending } = await mount();
    fireEvent.click(screen.getByRole("button", { name: "Rotate" }));
    fireEvent.click(apply);
    expect(encode).toHaveBeenCalledWith(
      "blob:synthetic-avatar", { x: 10, y: 20, width: 100, height: 100 }, 90,
      { output: 512, type: "image/webp", quality: 0.9, background: undefined },
    );
    await act(async () => pending.resolve(new Blob(["cropped"])));
  });
});
