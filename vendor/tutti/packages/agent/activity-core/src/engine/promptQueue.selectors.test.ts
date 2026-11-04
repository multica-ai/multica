import assert from "node:assert/strict";
import test from "node:test";
import { createInitialAgentSessionEngineState } from "./rootReducer.ts";
import { selectEngineHasQueuedPrompts } from "./promptQueue.selectors.ts";

test("workspace queued-prompt selector hides prompt queue storage shape", () => {
  const initial = createInitialAgentSessionEngineState();
  assert.equal(selectEngineHasQueuedPrompts(initial), false);

  const promptQueue = {
    ...initial.promptQueue,
    recordsBySessionId: { "session-1": { prompts: [{ id: "prompt-1" }] } }
  } as unknown as typeof initial.promptQueue;
  assert.equal(
    selectEngineHasQueuedPrompts({ ...initial, promptQueue }),
    true
  );
});
