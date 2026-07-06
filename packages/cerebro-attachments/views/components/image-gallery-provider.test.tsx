// @vitest-environment jsdom
// FIR-2710 — the surface gallery provider. Inline images and standalone chips
// on the same surface register via useGalleryImage; clicking any one opens a
// single ImageGallery paged through all of them. When no provider wraps the
// image (or the flag is off) the hook reports disabled so the caller keeps its
// own lightbox.
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

const flags: Record<string, boolean> = {};
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: (key: string) => flags[key] ?? false,
}));

import { ImageGalleryProvider, useGalleryImage } from "./image-gallery-provider";
import type { GalleryImage } from "@multica/cerebro-ui";

function Img({ image }: { image: GalleryImage }) {
  const g = useGalleryImage(image);
  return (
    <button
      aria-label={image.alt}
      data-enabled={g.enabled}
      ref={g.ref}
      onClick={g.open}
    />
  );
}

afterEach(() => {
  cleanup();
  for (const k of Object.keys(flags)) delete flags[k];
});

describe("ImageGalleryProvider + useGalleryImage", () => {
  it("opens one gallery paging through every registered image, at the clicked one", () => {
    flags.cerebro_image_gallery = true;
    render(
      <ImageGalleryProvider>
        <Img image={{ src: "a.png", alt: "A" }} />
        <Img image={{ src: "b.png", alt: "B" }} />
      </ImageGalleryProvider>,
    );

    expect(screen.queryByRole("dialog")).toBeNull();

    // Clicking the second image opens the gallery AT that image.
    fireEvent.click(screen.getByLabelText("B"));
    expect(screen.getByRole("dialog").getAttribute("aria-label")).toBe("B");

    // Both images live in the one gallery — paging back reaches the first.
    fireEvent.keyDown(document, { key: "ArrowLeft" });
    expect(screen.getByRole("dialog").getAttribute("aria-label")).toBe("A");
  });

  it("reports disabled (caller keeps its lightbox) when the flag is off", () => {
    flags.cerebro_image_gallery = false;
    render(
      <ImageGalleryProvider>
        <Img image={{ src: "a.png", alt: "A" }} />
      </ImageGalleryProvider>,
    );
    expect(screen.getByLabelText("A").getAttribute("data-enabled")).toBe("false");
    fireEvent.click(screen.getByLabelText("A"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("reports disabled when no provider wraps the image", () => {
    flags.cerebro_image_gallery = true;
    render(<Img image={{ src: "a.png", alt: "A" }} />);
    expect(screen.getByLabelText("A").getAttribute("data-enabled")).toBe("false");
    fireEvent.click(screen.getByLabelText("A"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
