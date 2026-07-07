/**
 * @vitest-environment jsdom
 */
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { MediaScrubber, formatTime, formatFrames, formatTimecode } from "./media-scrubber";

describe("MediaScrubber", () => {
  it("formats time correctly", () => {
    expect(formatTime(0)).toBe("0:00");
    expect(formatTime(65)).toBe("1:05");
    expect(formatTime(3605)).toBe("1:00:05");
  });

  it("formats frames correctly", () => {
    expect(formatFrames(0)).toBe("0");
    expect(formatFrames(1)).toBe("30");
  });

  it("formats timecode correctly", () => {
    expect(formatTimecode(0)).toBe("0:00.00");
    expect(formatTimecode(65.1)).toBe("1:05.09");
  });

  it("renders the scrubber track and handles clicks", () => {
    const onSeek = vi.fn();
    const { container } = render(
      <MediaScrubber 
        currentTime={10} 
        duration={100} 
        onSeek={onSeek} 
        streamUrl="http://example.com/test.mp4" 
      />
    );
    
    const track = container.querySelector(".group\\/progress");
    expect(track).toBeInTheDocument();
    
    if (track) {
      track.getBoundingClientRect = () => ({ left: 0, width: 100, top: 0, height: 10, bottom: 10, right: 100, x: 0, y: 0, toJSON: () => {} });
      fireEvent.pointerDown(track, { clientX: 50 });
      expect(onSeek).toHaveBeenCalled();
    }
  });
});
