// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook } from "@testing-library/react";
import {
  EMPTY_AGENT_DRAFT,
  toStoredAgentDraft,
  useManualAgentDraftStore,
  type AgentDraft,
} from "@multica/core/agents";
import { useManualDraftSync } from "./use-manual-draft-sync";

function seedStoredDraft(owner: string, name: string) {
  useManualAgentDraftStore.getState().setDraft({
    byOwner: {
      [owner]: {
        runtimeId: "runtime-1",
        draft: toStoredAgentDraft({ ...EMPTY_AGENT_DRAFT, name }, null),
      },
    },
  });
}

function mountSync(duplicateId: string | null, draft: AgentDraft) {
  const setDraft = vi.fn();
  const view = renderHook(() =>
    useManualDraftSync({ duplicateId, draft, setDraft, ready: true }),
  );
  return { setDraft, view };
}

afterEach(() => {
  cleanup();
  useManualAgentDraftStore.getState().clearDraft();
});

describe("useManualDraftSync", () => {
  it("restores the draft belonging to this flow", () => {
    seedStoredDraft("duplicate:agent-A", "Half-finished copy of A");

    const { setDraft } = mountSync("agent-A", EMPTY_AGENT_DRAFT);

    expect(setDraft).toHaveBeenCalledTimes(1);
    expect(setDraft.mock.calls[0]?.[0]).toMatchObject({
      name: "Half-finished copy of A",
      runtimeId: "runtime-1",
    });
  });

  // The regression: opening a second flow used to write its own blank form over
  // the first one's slot, destroying it before the user typed anything.
  it("does not touch another flow's draft when it opens", () => {
    seedStoredDraft("duplicate:agent-A", "Half-finished copy of A");

    const { setDraft } = mountSync(null, EMPTY_AGENT_DRAFT);

    expect(setDraft).not.toHaveBeenCalled();
    expect(
      useManualAgentDraftStore.getState().draft.byOwner["duplicate:agent-A"]
        ?.draft.name,
    ).toBe("Half-finished copy of A");
  });

  it("does not adopt another flow's draft either", () => {
    seedStoredDraft("duplicate:agent-A", "Half-finished copy of A");

    const { setDraft } = mountSync("agent-B", EMPTY_AGENT_DRAFT);

    expect(setDraft).not.toHaveBeenCalled();
    expect(
      useManualAgentDraftStore.getState().draft.byOwner["duplicate:agent-A"]
        ?.draft.name,
    ).toBe("Half-finished copy of A");
  });

  it("stores this flow's own edits beside the others", () => {
    seedStoredDraft("duplicate:agent-A", "Half-finished copy of A");

    mountSync(null, { ...EMPTY_AGENT_DRAFT, name: "A blank one" });

    const byOwner = useManualAgentDraftStore.getState().draft.byOwner;
    expect(byOwner.blank?.draft.name).toBe("A blank one");
    expect(byOwner["duplicate:agent-A"]?.draft.name).toBe(
      "Half-finished copy of A",
    );
  });
});
