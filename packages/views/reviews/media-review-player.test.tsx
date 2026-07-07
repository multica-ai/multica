/**
 * @vitest-environment jsdom
 */
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { MediaReviewPlayer } from "./media-review-player";
import type { ReviewAsset } from "@multica/core/types";

describe("MediaReviewPlayer", () => {
  it("renders a video element for video assets", () => {
    const asset: ReviewAsset = {
      id: "1",
      name: "test.mp4",
      src_url: "http://example.com/test.mp4",
      asset_type: "video",
      issue_id: "1",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      duration: 10,
    } as ReviewAsset;

    const { container } = render(<MediaReviewPlayer asset={asset} />);
    expect(container.querySelector("video")).toBeInTheDocument();
  });


  it("renders an image element for image assets", () => {
    const asset: ReviewAsset = {
      id: "3",
      name: "test.jpg",
      src_url: "http://example.com/test.jpg",
      asset_type: "image",
      issue_id: "1",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    } as ReviewAsset;

    const { container } = render(<MediaReviewPlayer asset={asset} />);
    expect(container.querySelector("img")).toBeInTheDocument();
  });

  it("toggles playback speed when clicking the speed button", () => {
    const asset: ReviewAsset = {
      id: "4",
      name: "test.mp4",
      src_url: "http://example.com/test.mp4",
      asset_type: "video",
      issue_id: "1",
      duration: 10,
    } as ReviewAsset;

    render(<MediaReviewPlayer asset={asset} />);
    const speedButton = screen.getByText("1x");
    
    // Default is 1x
    expect(speedButton.textContent).toBe("1x");
    
    // Click cycles to 1.25x
    fireEvent.click(speedButton);
    expect(screen.getByText("1.25x")).toBeInTheDocument();
    
    // Click cycles to 1.5x
    fireEvent.click(screen.getByText("1.25x"));
    expect(screen.getByText("1.5x")).toBeInTheDocument();
  });

  it("omits src attribute on video element when using HLS", () => {
    const asset: ReviewAsset = {
      id: "5",
      name: "test.m3u8",
      src_url: "http://example.com/test.m3u8",
      asset_type: "video",
      issue_id: "1",
      duration: 10,
    } as ReviewAsset;

    const { container } = render(<MediaReviewPlayer asset={asset} />);
    const video = container.querySelector("video");
    expect(video).toBeInTheDocument();
    expect(video?.hasAttribute("src")).toBe(false); // HLS overrides src
  });
});

