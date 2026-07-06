// @vitest-environment jsdom
// FIR-2710 — the image gallery lightbox pages through a message's images with
// prev/next + keyboard + a thumbnail strip, shows a counter, and zooms each.
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ImageGallery, type GalleryImage } from "./image-gallery";

const IMAGES: GalleryImage[] = [
  { src: "/a.png", alt: "img-1", downloadHref: "/a.png?download=1" },
  { src: "/b.png", alt: "img-2", downloadHref: "/b.png?download=1" },
  { src: "/c.png", alt: "img-3", downloadHref: "/c.png?download=1" },
];

function nav(name: "Previous image" | "Next image"): HTMLButtonElement {
  return screen.getByRole("button", { name }) as HTMLButtonElement;
}

// The main (large) image is the one whose alt is a real filename; thumbnails
// render with an empty alt, so alt-text uniquely identifies the current image.
function currentAlt(): string | null {
  const imgs = screen.getAllByRole("img") as HTMLImageElement[];
  const main = imgs.find((i) => i.alt !== "");
  return main?.alt ?? null;
}

afterEach(cleanup);

describe("ImageGallery", () => {
  it("renders nothing when closed", () => {
    render(<ImageGallery images={IMAGES} open={false} onClose={() => {}} />);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("opens at startIndex and shows the counter", () => {
    render(
      <ImageGallery images={IMAGES} startIndex={1} open onClose={() => {}} />,
    );
    expect(currentAlt()).toBe("img-2");
    // Counter text is split across text nodes — match on the container.
    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain("2 / 3");
  });

  it("pages forward and disables Next at the end", () => {
    render(
      <ImageGallery images={IMAGES} startIndex={1} open onClose={() => {}} />,
    );
    fireEvent.click(nav("Next image"));
    expect(currentAlt()).toBe("img-3");
    expect(nav("Next image").disabled).toBe(true);
    expect(nav("Previous image").disabled).toBe(false);
  });

  it("pages backward and disables Previous at the start", () => {
    render(
      <ImageGallery images={IMAGES} startIndex={1} open onClose={() => {}} />,
    );
    fireEvent.click(nav("Previous image"));
    expect(currentAlt()).toBe("img-1");
    expect(nav("Previous image").disabled).toBe(true);
  });

  it("navigates with the arrow keys", () => {
    render(
      <ImageGallery images={IMAGES} startIndex={0} open onClose={() => {}} />,
    );
    fireEvent.keyDown(document, { key: "ArrowRight" });
    expect(currentAlt()).toBe("img-2");
    fireEvent.keyDown(document, { key: "ArrowLeft" });
    expect(currentAlt()).toBe("img-1");
  });

  it("jumps to a thumbnail when its strip button is clicked", () => {
    render(
      <ImageGallery images={IMAGES} startIndex={0} open onClose={() => {}} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "View image 3" }));
    expect(currentAlt()).toBe("img-3");
  });

  it("closes on Escape and on the close button", () => {
    const onClose = vi.fn();
    render(<ImageGallery images={IMAGES} open onClose={onClose} />);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("hides pagination chrome for a single image", () => {
    render(
      <ImageGallery images={IMAGES.slice(0, 1)} open onClose={() => {}} />,
    );
    expect(screen.queryByRole("button", { name: "Next image" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Previous image" })).toBeNull();
    expect(screen.queryByRole("button", { name: /View image/ })).toBeNull();
    expect(screen.getByRole("dialog").textContent).not.toContain("/ 1");
  });

  it("clamps an out-of-range startIndex to the last image", () => {
    render(
      <ImageGallery images={IMAGES} startIndex={99} open onClose={() => {}} />,
    );
    expect(currentAlt()).toBe("img-3");
  });
});
