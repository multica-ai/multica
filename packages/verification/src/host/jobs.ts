import { randomUUID } from "node:crypto";
import { z } from "zod";
import {
  jobSchema,
  jsonSchema,
  receiptSchema,
  requestIdSchema,
  resourceIdSchema,
  type CommandResult,
  type Job,
  type Json,
  type Receipt,
} from "../contracts/index.js";
import { problem, toProblem, VerificationError } from "./problem.js";
import { canonicalJson, digest, HostStorage } from "./storage.js";

export interface OperationContext {
  signal: AbortSignal;
  addResource(id: string): Promise<void>;
  progress(phase: string): Promise<void>;
  awaitingUser(reason: string): Promise<void>;
}
export interface Operation {
  input: z.ZodType<Json>;
  output: z.ZodType<Json>;
  outputSchema: string;
  run(input: Json, context: OperationContext): Promise<Json>;
}
const terminal = (job: Job) =>
  ["completed", "failed", "cancelled", "interrupted", "unknown"].includes(
    job.state,
  );
type Execution = { controller: AbortController; active: boolean };

export class JobService {
  private readonly operations = new Map<string, Operation>();
  private readonly executions = new Map<string, Execution>();
  private readonly updates = new Map<string, Promise<unknown>>();
  private submissions: Promise<unknown> = Promise.resolve();
  private closing = false;

  private constructor(private readonly storage: HostStorage) {}

  static async open(storage: HostStorage): Promise<JobService> {
    const service = new JobService(storage);
    for (const file of await storage.list("jobs")) {
      const job = await storage.read(`jobs/${file}`, jobSchema);
      if (job && !terminal(job))
        await storage.write(
          `jobs/${file}`,
          jobSchema.parse({
            ...job,
            state: "interrupted",
            updatedAt: new Date().toISOString(),
            problem: problem(
              "outcomeUnknown",
              "The previous host stopped before confirming completion; inspect retained resources before retrying",
              "inspect",
              job.resourceRefs,
            ),
          }),
        );
    }
    service.register("host.cancel", {
      input: z.object({ jobId: resourceIdSchema }).strict(),
      output: z
        .object({ jobId: resourceIdSchema, state: jobSchema.shape.state })
        .strict(),
      outputSchema: "jobCancellation/v1",
      run: async (input) => {
        const { jobId } = z.object({ jobId: resourceIdSchema }).parse(input);
        const job = await service.update(jobId, (current) =>
          terminal(current) ? current : { ...current, state: "cancelling" },
        );
        if (!terminal(job)) service.executions.get(jobId)?.controller.abort();
        return { jobId, state: job.state };
      },
    });
    return service;
  }

  register(name: string, operation: Operation): void {
    if (this.operations.has(name))
      throw new Error(`Operation already registered: ${name}`);
    this.operations.set(name, operation);
  }

  async submit(
    name: string,
    input: Json,
    requestId: string,
  ): Promise<CommandResult> {
    const task = this.submissions
      .catch(() => undefined)
      .then(() => this.admit(name, input, requestId));
    this.submissions = task;
    return task;
  }

  private async admit(
    name: string,
    input: Json,
    requestId: string,
  ): Promise<CommandResult> {
    if (this.closing)
      throw new VerificationError(
        problem("interrupted", "The verification host is closing", "inspect"),
      );
    requestIdSchema.parse(requestId);
    const operation = this.operations.get(name);
    if (!operation)
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "The requested operation is not installed",
        ),
      );
    const parsed = jsonSchema.parse(operation.input.parse(input));
    const inputHash = digest(canonicalJson({ operation: name, input: parsed }));
    const receiptPath = `receipts/${digest(requestId)}.json`;
    const existing = await this.storage.read(receiptPath, receiptSchema);
    if (existing) {
      if (existing.operation !== name || existing.inputHash !== inputHash)
        throw new VerificationError(
          problem(
            "requestConflict",
            "This requestId already belongs to different input or another operation",
            "inspect",
            [existing.receiptId],
          ),
        );
      return this.result(existing);
    }
    const now = new Date().toISOString();
    const receipt: Receipt = {
      schemaVersion: 1,
      receiptId: randomUUID(),
      requestId,
      operation: name,
      inputHash,
      jobId: randomUUID(),
      createdAt: now,
    };
    const job: Job = {
      schemaVersion: 1,
      jobId: receipt.jobId,
      receiptId: receipt.receiptId,
      operation: name,
      resourceRefs: [],
      phase: "queued",
      state: "queued",
      createdAt: now,
      updatedAt: now,
    };
    // Execution starts only after both records exist. An orphan queued job is never replayed.
    await this.storage.write(`jobs/${job.jobId}.json`, jobSchema.parse(job));
    await this.storage.write(receiptPath, receiptSchema.parse(receipt));
    const execution: Execution = {
      controller: new AbortController(),
      active: true,
    };
    this.executions.set(job.jobId, execution);
    void this.execute(job, parsed, operation, execution);
    return {
      schemaVersion: 1,
      receiptId: receipt.receiptId,
      requestId,
      kind: "accepted",
      jobId: job.jobId,
    };
  }

  async get(jobId: string): Promise<Job> {
    resourceIdSchema.parse(jobId);
    const job = await this.storage.read(`jobs/${jobId}.json`, jobSchema);
    if (!job)
      throw new VerificationError(
        problem(
          "notFound",
          "The job does not exist in this project",
          "inspect",
        ),
      );
    return job;
  }

  /** Most recently updated jobs first. */
  async recent(limit = 50): Promise<Job[]> {
    const jobs: Job[] = [];
    for (const file of await this.storage.list("jobs")) {
      const job = await this.storage.read(`jobs/${file}`, jobSchema);
      if (job) jobs.push(job);
    }
    return jobs
      .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
      .slice(0, limit);
  }

  async getReceipt(receiptId: string): Promise<{ receipt: Receipt; job: Job }> {
    resourceIdSchema.parse(receiptId);
    for (const file of await this.storage.list("receipts")) {
      const receipt = await this.storage.read(
        `receipts/${file}`,
        receiptSchema,
      );
      if (receipt?.receiptId === receiptId)
        return { receipt, job: await this.get(receipt.jobId) };
    }
    throw new VerificationError(
      problem(
        "notFound",
        "The receipt does not exist in this project",
        "inspect",
      ),
    );
  }

  async byRequest(requestId: string): Promise<{ receipt: Receipt; job: Job }> {
    requestIdSchema.parse(requestId);
    const receipt = await this.storage.read(
      `receipts/${digest(requestId)}.json`,
      receiptSchema,
    );
    if (!receipt)
      throw new VerificationError(
        problem(
          "notFound",
          "The request has no receipt in this project",
          "inspect",
        ),
      );
    return { receipt, job: await this.get(receipt.jobId) };
  }

  cancel(jobId: string, requestId: string): Promise<CommandResult> {
    return this.submit("host.cancel", { jobId }, requestId);
  }

  private async result(receipt: Receipt): Promise<CommandResult> {
    const job = await this.get(receipt.jobId);
    const base = {
      schemaVersion: 1 as const,
      receiptId: receipt.receiptId,
      requestId: receipt.requestId,
    };
    if (job.state === "completed" && job.output)
      return { ...base, kind: "completed", value: job.output.value };
    if (job.state === "interrupted" || job.state === "unknown")
      return { ...base, kind: "unknown", problem: job.problem! };
    if (job.state === "failed" || job.state === "cancelled")
      return { ...base, kind: "rejected", problem: job.problem! };
    return { ...base, kind: "accepted", jobId: job.jobId };
  }

  private update(jobId: string, change: (job: Job) => Job): Promise<Job> {
    const next = (this.updates.get(jobId) ?? Promise.resolve())
      .catch(() => undefined)
      .then(async () => {
        const job = await this.get(jobId);
        const updated = jobSchema.parse({
          ...change(job),
          updatedAt: new Date().toISOString(),
        });
        await this.storage.write(`jobs/${jobId}.json`, updated);
        return updated;
      });
    this.updates.set(jobId, next);
    void next
      .finally(() => {
        if (this.updates.get(jobId) === next) this.updates.delete(jobId);
      })
      .catch(() => undefined);
    return next;
  }

  private async execute(
    job: Job,
    input: Json,
    operation: Operation,
    execution: Execution,
  ): Promise<void> {
    const assertActive = () => {
      if (!execution.active)
        throw new DOMException("The host stopped", "AbortError");
      execution.controller.signal.throwIfAborted();
    };
    const context: OperationContext = {
      signal: execution.controller.signal,
      addResource: async (id) => {
        assertActive();
        resourceIdSchema.parse(id);
        await this.update(job.jobId, (current) => {
          assertActive();
          return {
            ...current,
            resourceRefs: [...new Set([...current.resourceRefs, id])],
          };
        });
      },
      awaitingUser: async (reason) => {
        assertActive();
        await this.update(job.jobId, (current) => ({
          ...current,
          state: "awaitingUser",
          phase: reason,
        }));
      },
      progress: async (phase) => {
        assertActive();
        z.string().min(1).max(160).parse(phase);
        await this.update(job.jobId, (current) => {
          assertActive();
          return { ...current, phase };
        });
      },
    };
    try {
      await this.update(job.jobId, (current) => {
        assertActive();
        return { ...current, state: "running", phase: "running" };
      });
      assertActive();
      const value = await operation.run(input, context);
      const output = operation.output.safeParse(value);
      if (!output.success)
        throw new VerificationError(
          problem(
            "internalError",
            "The operation returned an invalid result",
            "inspect",
            [job.jobId],
          ),
        );
      await this.update(job.jobId, (current) =>
        execution.active
          ? {
              ...current,
              state: "completed",
              phase: "completed",
              output: {
                schema: operation.outputSchema,
                value: jsonSchema.parse(output.data),
              },
              problem: undefined,
            }
          : current,
      );
    } catch (error) {
      if (execution.active) {
        const failure = toProblem(error);
        await this.update(job.jobId, (current) =>
          execution.active
            ? {
                ...current,
                state:
                  failure.code === "outcomeUnknown"
                    ? "unknown"
                    : failure.code === "cancelled"
                      ? "cancelled"
                      : "failed",
                phase: "finished",
                problem: failure,
              }
            : current,
        ).catch(() => undefined);
      }
    } finally {
      if (this.executions.get(job.jobId) === execution)
        this.executions.delete(job.jobId);
    }
  }

  async close(): Promise<void> {
    this.closing = true;
    await this.submissions.catch(() => undefined);
    for (const [jobId, execution] of this.executions) {
      execution.active = false;
      execution.controller.abort();
      await this.update(jobId, (job) =>
        terminal(job)
          ? job
          : {
              ...job,
              state: "interrupted",
              phase: "interrupted",
              problem: problem(
                "outcomeUnknown",
                "The host stopped before confirming completion; inspect retained resources",
                "inspect",
                job.resourceRefs,
              ),
            },
      );
    }
    await Promise.allSettled(this.updates.values());
  }
}
