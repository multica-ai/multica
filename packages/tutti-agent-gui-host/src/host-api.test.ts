// @vitest-environment node
import { describe,expect,it,vi } from "vitest";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import { createAgentGuiHostApi } from "./host-api";
import type { AgentGuiHostServices } from "./protocol";

describe("host toast feedback", () => {
  it.each(["success", "info", "error"] as const)(
    "preserves %s through the host bridge",
    (tone) => {
      const request = vi.fn().mockResolvedValue(undefined);
      const host = createAgentGuiHostApi(
        fakeHostServices(request) as unknown as AgentGuiHostServices,
        { workspaceId: "workspace", locale: "zh-CN" },
        {
          getWorkspaceState: () => ({ agents: [], sessions: [] }),
          subscribeWorkspace: () => () => {}
        }
      );
      host.toast![tone]!("Clipboard feedback");
      expect(request).toHaveBeenCalledExactlyOnceWith("host.showMessage", {
        message: "Clipboard feedback",
        tone
      });
    }
  );
});
