import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { TaskTranscriptTimeline } from "./task-transcript-timeline";
import type { TimelineItem } from "./build-timeline";

const items: TimelineItem[] = [
  { seq: 1, type: "text", content: "Hello" },
  { seq: 2, type: "thinking", content: "Planning..." },
  { seq: 3, type: "tool_use", tool: "Bash", input: { command: "ls" } },
  { seq: 4, type: "tool_result", tool: "Bash", output: "file.txt" },
  { seq: 5, type: "error", content: "Something broke" },
];

describe("TaskTranscriptTimeline", () => {
  it("renders all event types", () => {
    render(<TaskTranscriptTimeline items={items} />);
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.getByText("Thinking")).toBeInTheDocument();
    expect(screen.getAllByText("Bash").length).toBe(2);
    expect(screen.getByText("Error")).toBeInTheDocument();
  });

  it("renders empty state when no items", () => {
    render(<TaskTranscriptTimeline items={[]} />);
    expect(screen.getByText("No events yet.")).toBeInTheDocument();
  });

  it("renders live empty state when isLive and no items", () => {
    render(<TaskTranscriptTimeline items={[]} isLive />);
    expect(screen.getByText("Waiting for events...")).toBeInTheDocument();
  });
});
