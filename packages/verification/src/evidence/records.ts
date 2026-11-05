import { randomUUID } from "node:crypto";
import { z } from "zod";
import {
  createVerificationInputSchema,
  toJson,
  resourceIdSchema,
  verificationSchema,
  type CommandResult,
  type VerificationRecord,
} from "../contracts/index.js";
import type { HostStorage } from "../host/storage.js";
import type { JobService } from "../host/jobs.js";
import { problem, VerificationError } from "../host/problem.js";

export class VerificationRecords {
  constructor(
    private readonly storage: HostStorage,
    private readonly jobs: JobService,
  ) {
    jobs.register("verification.create", {
      input: createVerificationInputSchema,
      output: z.object({ verificationId: resourceIdSchema }).strict(),
      outputSchema: "verificationCreated/v1",
      run: async (input, context) => {
        const data = createVerificationInputSchema.parse(input);
        const verificationId = randomUUID();
        await context.addResource(verificationId);
        context.signal.throwIfAborted();
        await storage.write(
          `verifications/${verificationId}.json`,
          verificationSchema.parse({
            schemaVersion: 1,
            verificationId,
            projectRef: storage.projectRef,
            ...data,
            state: "open",
            createdAt: new Date().toISOString(),
          }),
        );
        return { verificationId };
      },
    });
  }

  create(
    input: z.input<typeof createVerificationInputSchema>,
    requestId: string,
  ): Promise<CommandResult> {
    return this.jobs.submit(
      "verification.create",
      toJson(createVerificationInputSchema.parse(input)),
      requestId,
    );
  }

  async read(verificationId: string): Promise<VerificationRecord> {
    resourceIdSchema.parse(verificationId);
    const record = await this.storage.read(
      `verifications/${verificationId}.json`,
      verificationSchema,
    );
    if (!record)
      throw new VerificationError(
        problem(
          "notFound",
          "The verification record does not exist in this project",
        ),
      );
    if (record.projectRef !== this.storage.projectRef)
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The verification record belongs to another project",
        ),
      );
    return record;
  }
}
