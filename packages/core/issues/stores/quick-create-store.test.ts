import { beforeEach, describe, expect, it } from "vitest";
import { useQuickCreateStore } from "./quick-create-store";

const RESET_STATE = {
  lastAgentId: null,
  lastProjectId: null,
  prompt: "",
  keepOpen: false,
};

describe("quick create store", () => {
  beforeEach(() => {
    useQuickCreateStore.setState(RESET_STATE);
  });

  it("persists the agent prompt draft until explicitly cleared", () => {
    const { setPrompt, clearPrompt } = useQuickCreateStore.getState();

    setPrompt("Investigate the inbox loading regression");
    expect(useQuickCreateStore.getState().prompt).toBe(
      "Investigate the inbox loading regression",
    );

    clearPrompt();
    expect(useQuickCreateStore.getState().prompt).toBe("");
  });

  it("remembers the last project picked so frequent users skip the picker", () => {
    const { setLastProjectId } = useQuickCreateStore.getState();

    setLastProjectId("proj-1");
    expect(useQuickCreateStore.getState().lastProjectId).toBe("proj-1");

    setLastProjectId(null);
    expect(useQuickCreateStore.getState().lastProjectId).toBeNull();
  });
});
