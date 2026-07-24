export type TaskId = string;

export type FailureStrategy = "fail_fast" | "retry" | "skip";

export type TaskStatus = "pending" | "running" | "completed" | "failed" | "blocked" | "cancelled";

export interface TaskDefinition {
  id: TaskId;
  dependsOn?: TaskId[];
  failureStrategy?: FailureStrategy;
  maxRetries?: number;
}

export interface TaskSnapshot {
  id: TaskId;
  dependsOn: TaskId[];
  dependents: TaskId[];
  status: TaskStatus;
  failureStrategy: FailureStrategy;
  retriesRemaining: number;
}

