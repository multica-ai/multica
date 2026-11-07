// @vitest-environment node
import { expect, it } from "vitest";
import { sortWorkspaces } from "./sort-workspaces";
import type { Workspace } from "../types";

it("sorts pinned newest first and unpinned oldest first without mutating input", () => {
  const ws = (id: string, created_at: string, pinned_at: string | null = null) => ({ id, created_at, pinned_at }) as Workspace;
  const input = [ws("late", "2026-02-01"), ws("early", "2026-01-01"), ws("old-pin", "2026-01-01", "2026-03-01"), ws("new-pin", "2026-01-01", "2026-04-01")];
  expect(sortWorkspaces(input).map((item) => item.id)).toEqual(["new-pin", "old-pin", "early", "late"]);
  expect(input[0]?.id).toBe("late");
});
