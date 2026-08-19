import { describe, expect, it, vi } from "vitest";

const { redirect } = vi.hoisted(() => ({
  redirect: vi.fn((destination: string) => {
    throw new Error(`redirect:${destination}`);
  }),
}));

vi.mock("next/navigation", () => ({ redirect }));

import NewAgentRoute from "./page";
import NewAgentAiRoute from "./ai/page";
import NewAgentAiSessionRoute from "./ai/[sessionId]/page";

describe("retired Agent creation routes", () => {
  it("sends the old chooser directly to approved manual creation", async () => {
    await expect(
      NewAgentRoute({ params: Promise.resolve({ workspaceSlug: "my team" }) }),
    ).rejects.toThrow("redirect:/my%20team/agents/new/manual");
  });

  it.each([
    ["AI builder", NewAgentAiRoute],
    ["AI builder session", NewAgentAiSessionRoute],
  ])("sends the old %s route back to Agents", async (_label, route) => {
    await expect(
      route({
        params: Promise.resolve({
          workspaceSlug: "my team",
          sessionId: "session-1",
        }),
      }),
    ).rejects.toThrow("redirect:/my%20team/agents");
  });
});
