import { beforeEach, describe, expect, it } from "vitest";
import { useQuickCreateStore } from "./quick-create-store";

const RESET_STATE = {
  lastActorType: null,
  lastActorId: null,
  keepOpen: false,
};

describe("quick create store", () => {
  beforeEach(() => {
    useQuickCreateStore.setState(RESET_STATE);
  });

  // The store remembers the actor and nothing else about the last create.
  // Project is a property of the issue being filed, not a standing
  // preference, so carrying it forward files the next issue somewhere the
  // user never picked (MUL-5862).
  it("does not carry any project memory", () => {
    expect(useQuickCreateStore.getState()).not.toHaveProperty("lastProjectId");
    expect(useQuickCreateStore.getState()).not.toHaveProperty("setLastProjectId");
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
