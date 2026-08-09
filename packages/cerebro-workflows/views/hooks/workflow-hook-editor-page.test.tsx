// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { HookLoadError } from "./workflow-hook-editor-page";

afterEach(cleanup);

describe("WorkflowHookEditorPage", () => {
  it("keeps a malformed Hook non-editable and offers Retry", () => {
    const retry = vi.fn();
    render(<HookLoadError onRetry={retry} />);

    expect(screen.getByRole("alert")).toHaveTextContent("The Hook could not be read");
    screen.getByRole("button", { name: "Retry" }).click();
    expect(retry).toHaveBeenCalledOnce();
  });
});
