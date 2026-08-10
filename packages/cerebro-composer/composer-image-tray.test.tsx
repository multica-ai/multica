import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ComposerImageTray } from "./composer-image-tray";
import type { ImageTrayItem } from "./use-image-tray";

const completed: ImageTrayItem = {
  localId: "image-1",
  blobUrl: "blob:image-1",
  filename: "phone.png",
  status: "completed",
  uploadedUrl: "https://cdn.example/phone.png",
};

describe("ComposerImageTray phone controls", () => {
  it("gives place and remove separate 44 px touch targets", () => {
    const onEmbed = vi.fn();
    const onRemove = vi.fn();

    render(
      <ComposerImageTray
        items={[completed]}
        onPreview={vi.fn()}
        onEmbed={onEmbed}
        onRemove={onRemove}
        embedLabel="Place in text"
      />,
    );

    const place = screen
      .getAllByRole("button", { name: "Place image 1 in text" })
      .find((button) => button.classList.contains("size-11"));
    const remove = screen.getByRole("button", { name: "Remove image 1" });

    expect(place).toBeDefined();
    if (!place) throw new Error("Phone Place in text action not found");
    expect(place).toHaveClass("size-11", "md:hidden");
    expect(remove).toHaveClass("size-11", "md:hidden");
    expect(screen.getByRole("listitem")).toHaveClass("max-md:w-40");
    expect(
      screen.getByRole("button", { name: "Remove attachment" }),
    ).toHaveClass("max-md:hidden");
    expect(
      screen.getByRole("button", { name: "Preview image 1" }).parentElement,
    ).toHaveClass("max-md:mx-auto");

    fireEvent.click(place);
    fireEvent.click(remove);

    expect(onEmbed).toHaveBeenCalledWith(completed);
    expect(onRemove).toHaveBeenCalledWith("image-1");
  });

  it("gives a failed image a 44 px remove target", () => {
    render(
      <ComposerImageTray
        items={[{ ...completed, status: "failed" }]}
        onPreview={vi.fn()}
        onEmbed={vi.fn()}
        onRemove={vi.fn()}
      />,
    );

    const remove = screen
      .getAllByRole("button", { name: "Remove failed image 1" })
      .find((button) => button.classList.contains("size-11"));
    expect(remove).toBeDefined();
    expect(remove).toHaveClass("size-11", "md:hidden");
  });
});
