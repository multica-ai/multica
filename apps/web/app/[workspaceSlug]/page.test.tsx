import { describe, expect, it, vi } from "vitest";
import { paths } from "@multica/core/paths";

const { mockRedirect } = vi.hoisted(() => ({
  mockRedirect: vi.fn((path: string) => {
    throw new Error(`redirect:${path}`);
  }),
}));

vi.mock("next/navigation", () => ({
  redirect: mockRedirect,
}));

import WorkspaceRootPage from "./page";

describe("WorkspaceRootPage", () => {
  it("makes the bare workspace slug route valid by redirecting to the workspace root", async () => {
    await expect(
      WorkspaceRootPage({
        params: Promise.resolve({ workspaceSlug: "jeh-b0edd870" }),
      }),
    ).rejects.toThrow(`redirect:${paths.workspace("jeh-b0edd870").root()}`);

    expect(mockRedirect).toHaveBeenCalledWith(
      paths.workspace("jeh-b0edd870").issues(),
    );
  });
});
