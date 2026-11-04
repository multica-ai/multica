import type {
  AgentSessionEngine,
  AgentSessionEngineState,
  EngineIntent
} from "../index.ts";

type Assert<T extends true> = T;
type IsEqual<Left, Right> = [Left, Right] extends [Right, Left] ? true : false;
type IsNever<T> = [T] extends [never] ? true : false;

export type PromptExecutionIntentRemainsPrivate = Assert<
  IsNever<Extract<EngineIntent, { type: "prompt/executionRequested" }>>
>;

export type PromptExecutionStateRemainsPrivate = Assert<
  "promptExecutions" extends keyof AgentSessionEngineState ? false : true
>;

