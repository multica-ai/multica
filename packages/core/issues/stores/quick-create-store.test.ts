import { beforeEach, describe, expect, it } from "vitest";
import { useQuickCreateStore } from "./quick-create-store";

const RESET_STATE = {
  lastActorType: null,
  lastActorId: null,
  lastProjectId: null,
  keepOpen: false,
};

describe("quick create store", () => {
  beforeEach(() => {
    useQuickCreateStore.setState(RESET_STATE);
  });

  it("remembers the last project picked so frequent users skip the picker", () => {
    const { setLastProjectId } = useQuickCreateStore.getState();

    setLastProjectId("proj-1");
    expect(useQuickCreateStore.getState().lastProjectId).toBe("proj-1");

    setLastProjectId(null);
    expect(useQuickCreateStore.getState().lastProjectId).toBeNull();
  });

  it("remembers the last actor (agent or squad) and clears both fields together", () => {
    const { setLastActor } = useQuickCreateStore.getState();

    setLastActor("agent", "agent-1");
    expect(useQuickCreateStore.getState().lastActorType).toBe("agent");
    expect(useQuickCreateStore.getState().lastActorId).toBe("agent-1");

    setLastActor("squad", "squad-1");
    expect(useQuickCreateStore.getState().lastActorType).toBe("squad");
    expect(useQuickCreateStore.getState().lastActorId).toBe("squad-1");

    setLastActor(null, null);
    expect(useQuickCreateStore.getState().lastActorType).toBeNull();
    expect(useQuickCreateStore.getState().lastActorId).toBeNull();
  });
});
