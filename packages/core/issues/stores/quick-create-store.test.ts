import { beforeEach, describe, expect, it } from "vitest";
import { rankFrequentActors, useQuickCreateStore } from "./quick-create-store";

const RESET_STATE = {
  lastActorType: null,
  lastActorId: null,
  actorCreateCounts: {},
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

  it("counts creates per actor, keeping an agent and a squad with the same id apart", () => {
    const { countActorCreate } = useQuickCreateStore.getState();

    countActorCreate("agent", "a");
    countActorCreate("agent", "a");
    countActorCreate("squad", "a");

    expect(useQuickCreateStore.getState().actorCreateCounts).toEqual({
      "agent:a": 2,
      "squad:a": 1,
    });
  });
});

describe("rankFrequentActors", () => {
  const agent = (id: string) => ({ type: "agent" as const, id });
  const squad = (id: string) => ({ type: "squad" as const, id });

  it("puts the actors with the most creates first", () => {
    const counts = { "agent:a": 1, "agent:c": 5, "squad:s": 3 };
    expect(rankFrequentActors(counts, [agent("a"), agent("b"), agent("c"), squad("s")], 3))
      .toEqual([agent("c"), squad("s"), agent("a")]);
  });

  it("fills slots without counts, and breaks ties, in candidate order", () => {
    const counts = { "agent:c": 2, "agent:b": 2 };
    expect(rankFrequentActors(counts, [agent("a"), agent("b"), agent("c"), agent("d")], 3))
      .toEqual([agent("b"), agent("c"), agent("a")]);
    expect(rankFrequentActors({}, [agent("a"), agent("b")], 3)).toEqual([agent("a"), agent("b")]);
  });

  it("ignores counts for actors that are no longer candidates", () => {
    const counts = { "agent:gone": 9, "agent:b": 1 };
    expect(rankFrequentActors(counts, [agent("a"), agent("b")], 3)).toEqual([agent("b"), agent("a")]);
  });
});
