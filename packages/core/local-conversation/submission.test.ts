import {persistedQueuedPromptSchema} from "./submission-storage";
import {LOCAL_REASONING_EFFORTS} from "./protocol";
// @vitest-environment node
import { expect, it } from "vitest";
import {
  localSubmissionSchema,
  localSubmissionStatusesSchema,
  mergeSubmission,
} from "./submission";
it("keeps unknown authoritative states and delivery outcomes conservative", () => {
  const value = localSubmissionSchema.parse({
    request_id: "r",
    revision: 4,
    state: "future-state",
    attempt: { attempt_id: "a", request_id: "r", delivery: "future-delivery" },
  });
  expect(value.state).toBe("recovery_required");
  expect(value.attempt?.delivery).toBe("unknown");
  expect(
    mergeSubmission(value, { ...value, revision: 3, state: "queued" }),
  ).toBe(value);
  expect(
    localSubmissionStatusesSchema.parse({
      store_generation: "g",
      snapshot_seq: 10,
      queue_revision: 1,
    }).known_history,
  ).toBe(false);
  expect(
    localSubmissionSchema.safeParse({
      request_id: "r",
      revision: -1,
      state: "queued",
    }).success,
  ).toBe(false);
});

it("persists every reasoning effort the conversation protocol supports",()=>{
 for(const reasoningEffort of LOCAL_REASONING_EFFORTS)expect(persistedQueuedPromptSchema.parse({requestId:"r",content:"message",reasoningEffort,display:{id:"r",createdAtUnixMs:1,content:[{type:"text",text:"message"}]}}).reasoningEffort).toBe(reasoningEffort);
});
