// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, it, expect, vi } from "vitest";
import { renderWithI18n } from "../../../test/i18n";
import { maxSiblingStage, stageOptions, StagePicker } from "./stage-picker";

afterEach(cleanup);

describe("maxSiblingStage", () => {
  it("returns 0 when nothing is staged", () => {
    expect(maxSiblingStage([])).toBe(0);
    expect(maxSiblingStage([{ stage: null }, { stage: null }])).toBe(0);
  });

  it("returns the highest assigned stage, ignoring unstaged children", () => {
    expect(
      maxSiblingStage([{ stage: 1 }, { stage: 5 }, { stage: null }, { stage: 3 }]),
    ).toBe(5);
  });
});

describe("stageOptions", () => {
  it("floors at Stage 1–5 by default when nothing higher exists", () => {
    expect(stageOptions(null, 0)).toEqual([1, 2, 3, 4, 5]);
    expect(stageOptions(1, 0)).toEqual([1, 2, 3, 4, 5]);
  });

  it("honors a custom floor when provided", () => {
    expect(stageOptions(null, 0, 3)).toEqual([1, 2, 3]);
  });

  // The regression Elon flagged: a parent already has a Stage 5 child, so the
  // picker must keep Stage 5 selectable and offer a new Stage 6 — not floor at 3.
  it("extends one beyond the highest sibling stage", () => {
    expect(stageOptions(null, 5)).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it("extends one beyond the current stage even without siblings", () => {
    expect(stageOptions(5, 0)).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it("extends further when higher stages are present", () => {
    expect(stageOptions(null, 7)).toEqual([1, 2, 3, 4, 5, 6, 7, 8]);
  });
});

describe("StagePicker component", () => {
  it("renders Stage 1 to 5 options by default when creating a sub-issue", () => {
    renderWithI18n(
      <StagePicker
        stage={null}
        onUpdate={() => {}}
        maxStage={0}
        open
        onOpenChange={() => {}}
      />,
    );

    expect(screen.getAllByText("No stage").length).toBe(2);
    expect(screen.getByText("Stage 1")).toBeTruthy();
    expect(screen.getByText("Stage 2")).toBeTruthy();
    expect(screen.getByText("Stage 3")).toBeTruthy();
    expect(screen.getByText("Stage 4")).toBeTruthy();
    expect(screen.getByText("Stage 5")).toBeTruthy();
    expect(screen.queryByText("Stage 6")).toBeNull();
    expect(screen.getByRole("button", { name: "Add stage" })).toBeTruthy();
  });

  it("reveals the next stage when clicking Add stage", () => {
    renderWithI18n(
      <StagePicker
        stage={null}
        onUpdate={() => {}}
        maxStage={0}
        open
        onOpenChange={() => {}}
      />,
    );

    const addStageButton = screen.getByRole("button", { name: "Add stage" });
    fireEvent.click(addStageButton);

    expect(screen.getByText("Stage 6")).toBeTruthy();

    fireEvent.click(addStageButton);
    expect(screen.getByText("Stage 7")).toBeTruthy();
  });

  it("selects a stage and closes picker on click", () => {
    const onUpdate = vi.fn();
    const onOpenChange = vi.fn();
    renderWithI18n(
      <StagePicker
        stage={null}
        onUpdate={onUpdate}
        maxStage={0}
        open
        onOpenChange={onOpenChange}
      />,
    );

    fireEvent.click(screen.getByText("Stage 4"));
    expect(onUpdate).toHaveBeenCalledWith({ stage: 4 });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
