import { pageEvents, eventFilterSchema } from "./event-page.js";
import { randomUUID } from "node:crypto";
import { z } from "zod";
import WebSocket from "ws";
import { jsonSchema, resourceIdSchema } from "../contracts/index.js";
import {
  eventSchema,
  observationSchema,
  stateSnapshotSchema,
  stepSchema,
  type ObservedEvent,
  type StateSnapshot,
  type Step,
} from "../contracts/resources.js";
import type { OperationContext } from "../host/jobs.js";
import type { HostStorage } from "../host/storage.js";
import type { Environments } from "../setup/environments.js";
import type { EvidenceStore } from "../evidence/store.js";
import type { VerificationRecords } from "../evidence/records.js";
import { problem, toProblem, VerificationError } from "../host/problem.js";

export type SourceKind = "daemon" | "main" | "renderer" | "backend" | "page";
const snapshotWire = z
  .object({
    version: z.literal(2),
    store_generation: z.string(),
    snapshot_seq: z.number().int().nonnegative(),
    conversation: z.object({ conversation_id: z.string() }).passthrough(),
    turns: z.array(
      z.object({ id: z.string(), status: z.string() }).passthrough(),
    ),
    queue: z
      .object({
        revision: z.number().int().nonnegative(),
        paused: z.boolean(),
        items: z.array(jsonSchema),
      })
      .passthrough(),
  })
  .passthrough();
type Active = {
  sockets: WebSocket[];
  timer: ReturnType<typeof setInterval>;
  busy: Promise<unknown>;
  stopping: boolean;
};
export class Sources {
  private readonly active = new Map<string, Active>();
  private writes: Promise<unknown> = Promise.resolve();
  constructor(
    private readonly storage: HostStorage,
    readonly environments: Environments,
    private readonly evidence: EvidenceStore,
    private readonly records: VerificationRecords,
  ) {}
  async recover(): Promise<void> {
    for (const name of await this.storage.list("steps")) {
      const step = await this.storage.read(`steps/${name}`, stepSchema);
      if (step?.state === "observing") {
        step.state = "interrupted";
        step.coverage = "partial";
        step.gaps.push("collectorRestarted");
        await this.save(step);
      }
    }
  }
  async capabilities(environmentId: string) {
    const result: Record<string, unknown> = {};
    const environment = await this.environments.get(environmentId);
    for (const source of (environment.dataMode === "liveReadOnly"
      ? ["page"]
      : ["daemon", "main"]) as ("page" | "daemon" | "main")[])
      try {
        result[source] =
          source === "daemon"
            ? await this.environments.bindings.request(
                await this.environments.connection(environmentId, "daemon"),
                "/local/v2/verification/capabilities",
              )
            : await this.environments.desktopRequest(
                environmentId,
                "/capabilities",
              );
      } catch (error) {
        result[source] = { status: "unavailable", problem: toProblem(error) };
      }
    return result;
  }
  async snapshot(
    environmentId: string,
    subjectRef: string,
    sources: SourceKind[] = ["daemon"],
  ): Promise<StateSnapshot> {
    await this.environments.get(environmentId);
    const observations = await Promise.all(
      sources.map(async (source) => {
        const startedAt = new Date().toISOString();
        let instance = environmentId;
        try {
          let payload: unknown,
            generation: string | undefined,
            cursor: number | undefined,
            partial = false;
          if (source === "daemon") {
            const ref = await this.environments.connection(
                environmentId,
                "daemon",
              ),
              binding = this.environments.bindings.get(ref);
            const health = z
              .object({
                pid: z.number(),
                instance_owner_id: z.string(),
                instance_run_id: z.string(),
                cli_commit: z.string(),
                server_url: z.string(),
              })
              .passthrough()
              .parse(await this.environments.bindings.request(ref, "/health"));
            if (
              (binding.identity.pid && health.pid !== binding.identity.pid) ||
              (binding.identity.ownerId &&
                health.instance_owner_id !== binding.identity.ownerId) ||
              (binding.identity.runId &&
                health.instance_run_id !== binding.identity.runId) ||
              (binding.identity.commit &&
                !health.cli_commit.startsWith(binding.identity.commit) &&
                !binding.identity.commit.startsWith(health.cli_commit)) ||
              health.server_url !== binding.scope.apiOrigin
            )
              throw new VerificationError(
                problem(
                  "identityMismatch",
                  "The daemon identity differs from its binding",
                ),
              );
            instance = `${health.instance_owner_id}:${health.instance_run_id}`;
            const wire = snapshotWire.parse(
              await this.environments.bindings.request(
                ref,
                `/local/v2/sessions/${encodeURIComponent(subjectRef)}/snapshot`,
              ),
            );
            generation = wire.store_generation;
            cursor = wire.snapshot_seq;
            let fixture: unknown;
            try {
              fixture = await this.environments.bindings.request(
                ref,
                `/local/v2/verification/fixture?conversationId=${encodeURIComponent(subjectRef)}`,
              );
            } catch {
              /* Not every source has a fake provider. */
            }
            payload = { ...wire, ...(fixture ? { fixture } : {}) };
          } else if (source === "backend") {
            const match = /^(issue|project):(.+)$/.exec(subjectRef);
            if (!match)
              throw new VerificationError(
                problem(
                  "unsupportedCapability",
                  "Backend observations require issue: or project: references",
                ),
              );
            const ref = await this.environments.connection(
              environmentId,
              "backend",
            );
            payload = await this.environments.bindings.request(
              ref,
              `/api/${match[1]}s/${encodeURIComponent(match[2]!)}`,
            );
            instance = this.environments.bindings.get(ref).scope.apiOrigin;
          } else {
            const descriptor =
              await this.environments.descriptor(environmentId);
            instance = `${descriptor.ownerId}:${descriptor.pid}`;
            const wire = z
              .object({
                schemaVersion: z.literal(1),
                environmentId: z.string(),
                generation: z.string(),
                cursor: z.number().int().nonnegative(),
                payload: jsonSchema,
              })
              .parse(
                await this.environments.desktopRequest(
                  environmentId,
                  `/snapshot?source=${source}&subjectRef=${encodeURIComponent(subjectRef)}`,
                ),
              );
            if (wire.environmentId !== environmentId)
              throw new VerificationError(
                problem(
                  "identityMismatch",
                  "The diagnostic endpoint changed environments",
                ),
              );
            payload = wire.payload;
            generation = wire.generation;
            cursor = wire.cursor;
            partial = !!(
              payload &&
              typeof payload === "object" &&
              !Array.isArray(payload) &&
              (("unavailable" in payload && payload.unavailable) ||
                ("conversationUnavailable" in payload &&
                  payload.conversationUnavailable))
            );
          }
          return observationSchema.parse({
            source,
            status: partial ? "partial" : "available",
            instance,
            startedAt,
            endedAt: new Date().toISOString(),
            generation,
            cursor,
            payload: jsonSchema.parse(payload),
          });
        } catch (error) {
          return observationSchema.parse({
            source,
            status: "unavailable",
            instance,
            startedAt,
            endedAt: new Date().toISOString(),
            problem: toProblem(error),
          });
        }
      }),
    );
    const snapshot = stateSnapshotSchema.parse({
      schemaVersion: 1,
      stateSnapshotId: randomUUID(),
      environmentId,
      subjectRef,
      consistency: "perSource",
      observations,
    });
    await this.storage.write(
      `state-snapshots/${snapshot.stateSnapshotId}.json`,
      snapshot,
    );
    return snapshot;
  }
  async begin(
    input: {
      verificationId: string;
      environmentId: string;
      subjectRef: string;
      description: string;
      driver: Step["driver"];
      sources: SourceKind[];
      optionalSources?: SourceKind[];
    },
    context?: OperationContext,
  ): Promise<Step> {
    if (this.active.size >= 32)
      throw new VerificationError(
        problem("instanceBusy", "At most 32 observation steps may be active"),
      );
    const optionalSources = input.optionalSources ?? [];
    if (optionalSources.some((source) => input.sources.includes(source)))
      throw new VerificationError(
        problem(
          "invalidInput",
          "Required and optional sources must be disjoint",
        ),
      );
    if (input.sources.includes("renderer") && !input.sources.includes("main"))
      input = { ...input, sources: [...input.sources, "main"] };
    const record = await this.records.read(input.verificationId);
    if (
      !record.environments.some(
        (entry) => entry.environmentId === input.environmentId,
      )
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "Bind the environment to this verification before starting observation",
        ),
      );
    const stepId = randomUUID();
    await context?.addResource(stepId);
    const snapshot = await this.snapshot(
      input.environmentId,
      input.subjectRef,
      [...input.sources, ...optionalSources],
    );
    const step: Step = {
      schemaVersion: 1,
      stepId,
      verificationId: input.verificationId,
      environmentId: input.environmentId,
      subjectRef: input.subjectRef,
      description: input.description,
      dataGeneration: String(
        (await this.environments.get(input.environmentId)).components
          .dataGeneration ?? "initial",
      ),
      driver: input.driver,
      optionalSources,
      state: "observing",
      startedAt: new Date().toISOString(),
      startSnapshotId: snapshot.stateSnapshotId,
      startCursors: {},
      endCursors: {},
      generations: {},
      coverage: "complete",
      gaps: [],
      events: [],
    };
    for (const observation of snapshot.observations) {
      if (
        observation.status !== "available" ||
        observation.cursor === undefined ||
        !observation.generation
      ) {
        if (!optionalSources.includes(observation.source))
          step.coverage = "partial";
        step.gaps.push(
          `${optionalSources.includes(observation.source) ? "optional." : ""}${observation.source}:unavailable`,
        );
        continue;
      }
      step.startCursors[observation.source] = observation.cursor;
      step.endCursors[observation.source] = observation.cursor;
      step.generations[observation.source] =
        `${observation.generation}:${observation.instance}`;
    }
    await this.save(step);
    await this.evidence.attach({
      verificationId: step.verificationId,
      environmentId: step.environmentId,
      stepId: step.stepId,
      kind: "snapshot",
      source: "step.begin",
      mediaType: "application/json",
      coverage: step.coverage,
      bytes: Buffer.from(JSON.stringify(snapshot)),
    });
    const active: Active = {
      sockets: [],
      timer: setInterval(() => {
        if (!active.stopping)
          active.busy = active.busy
            .then(() => this.collect(step.stepId))
            .catch(() => undefined);
      }, 250),
      busy: Promise.resolve(),
      stopping: false,
    };
    active.timer.unref();
    this.active.set(step.stepId, active);
    for (const observation of snapshot.observations.filter(
      (item) =>
        item.status === "available" &&
        item.source !== "backend" &&
        item.source !== "page",
    )) {
      try {
        let url: string, headers: Record<string, string>;
        if (observation.source === "daemon") {
          const ref = await this.environments.connection(
              step.environmentId,
              "daemon",
            ),
            binding = this.environments.bindings.get(ref);
          url =
            binding.baseUrl.replace(/^http/, "ws") +
            `/local/v2/verification/ws?conversationId=${encodeURIComponent(step.subjectRef)}&after=${observation.cursor}&generation=${encodeURIComponent(observation.generation!)}`;
          headers = await this.environments.bindings.headers(ref);
        } else {
          if (
            observation.source === "renderer" &&
            snapshot.observations.some((item) => item.source === "main")
          )
            continue;
          const connection = await this.environments.diagnosticConnection(
            step.environmentId,
          );
          url = connection.url.replace(/^http/, "ws") + "/events/ws";
          headers = connection.headers;
        }
        const socket = new WebSocket(url, { headers });
        active.sockets.push(socket);
        await new Promise<void>((resolve, reject) => {
          const timer = setTimeout(
            () => reject(new Error("Observation handshake timed out")),
            3000,
          );
          socket.once("error", (error) => {
            clearTimeout(timer);
            reject(error);
          });
          socket.on("message", (data) => {
            try {
              const value = JSON.parse(data.toString());
              if (value.type === "observation.ready") {
                clearTimeout(timer);
                resolve();
              } else if (!active.stopping)
                active.busy = active.busy
                  .then(() => this.collect(step.stepId))
                  .catch(() => undefined);
            } catch {
              /* HTTP catch-up verifies authoritative event data. */
            }
          });
        });
        socket.on("error", () => undefined);
      } catch {
        /* Durable HTTP catch-up can cover a disconnected event stream. */
      }
    }
    await this.collect(step.stepId);
    return this.get(step.stepId);
  }
  async get(id: string): Promise<Step> {
    const step = await this.storage.read(
      `steps/${resourceIdSchema.parse(id)}.json`,
      stepSchema,
    );
    if (!step)
      throw new VerificationError(
        problem("notFound", "The observation step does not exist"),
      );
    return step;
  }
  private save(step: Step) {
    return this.storage.write(
      `steps/${step.stepId}.json`,
      stepSchema.parse(step),
    );
  }
  private collect(id: string, through?: Record<string, number>): Promise<void> {
    const next = this.writes
      .catch(() => undefined)
      .then(async () => {
        const step = await this.get(id);
        if (step.state !== "observing") return;
        const initial = await this.storage.read(
          `state-snapshots/${step.startSnapshotId}.json`,
          stateSnapshotSchema,
        );
        if (!initial) throw new Error("Missing initial snapshot");
        for (const source of Object.keys(step.startCursors)) {
          if (source === "backend" || source === "renderer") continue;
          try {
            const observation = initial.observations.find(
              (item) => item.source === source,
            )!;
            for (let page = 0; page < 100; page++) {
              const after = step.endCursors[source]!;
              const raw =
                source === "daemon"
                  ? await this.environments.bindings.request(
                      await this.environments.connection(
                        step.environmentId,
                        "daemon",
                      ),
                      `/local/v2/verification/events?conversationId=${encodeURIComponent(step.subjectRef)}&after=${after}&generation=${encodeURIComponent(observation.generation!)}`,
                    )
                  : await this.environments.desktopRequest(
                      step.environmentId,
                      `/events?after=${after}`,
                    );
              const wire = z
                .object({
                  schemaVersion: z.literal(1),
                  generation: z.string(),
                  cursor: z.number().int().nonnegative(),
                  highWater: z.number().optional(),
                  earliestCursor: z.number().optional(),
                  hasMore: z.boolean(),
                  events: z.array(jsonSchema),
                })
                .parse(raw);
              if (
                wire.generation !== observation.generation ||
                wire.cursor < after ||
                (wire.earliestCursor !== undefined &&
                  after < wire.earliestCursor - 1)
              )
                throw new VerificationError(
                  problem(
                    "coverageIncomplete",
                    "Event generation changed or retained events were lost",
                  ),
                );
              for (const value of wire.events) {
                const raw = z.record(z.string(), jsonSchema).parse(value);
                const cursor = z
                  .number()
                  .int()
                  .positive()
                  .parse(source === "daemon" ? raw.seq : raw.cursor);
                if (
                  through?.[source] !== undefined &&
                  cursor > through[source]!
                )
                  break;
                if (cursor <= after) continue;
                if (cursor !== step.endCursors[source]! + 1)
                  throw new VerificationError(
                    problem(
                      "coverageIncomplete",
                      "The source event sequence has a gap",
                    ),
                  );
                const event: ObservedEvent = eventSchema.parse({
                  eventId: `${source}:${observation.generation}:${cursor}`,
                  source,
                  instance: observation.instance,
                  generation: observation.generation,
                  cursor,
                  type: z.string().parse(raw.type),
                  occurredAt:
                    typeof raw.created_at === "string"
                      ? raw.created_at
                      : typeof raw.occurredAt === "string"
                        ? raw.occurredAt
                        : undefined,
                  receivedAt: new Date().toISOString(),
                  ...(source === "daemon"
                    ? { conversationId: step.subjectRef }
                    : {}),
                  ...(typeof raw.turn_id === "string"
                    ? { turnId: raw.turn_id }
                    : {}),
                  ...(typeof raw.request_id === "string"
                    ? { requestId: raw.request_id }
                    : {}),
                  payload: raw.payload ?? null,
                });
                step.events.push(event);
                step.endCursors[source] = cursor;
                if (step.events.length > 10000)
                  throw new VerificationError(
                    problem(
                      "coverageIncomplete",
                      "The observation exceeded its event limit",
                    ),
                  );
              }
              if (
                !wire.hasMore ||
                (through?.[source] !== undefined &&
                  step.endCursors[source]! >= through[source]!)
              )
                break;
              if (page === 99)
                throw new VerificationError(
                  problem(
                    "coverageIncomplete",
                    "The event catch-up limit was exceeded",
                  ),
                );
            }
          } catch {
            if (!step.optionalSources.includes(source as SourceKind))
              step.coverage = "partial";
            if (!step.gaps.includes(`${source}:coverageIncomplete`))
              step.gaps.push(`${source}:coverageIncomplete`);
          }
        }
        await this.save(step);
      });
    this.writes = next;
    return next;
  }
  async end(id: string): Promise<Step> {
    const active = this.active.get(id);
    if (active) {
      active.stopping = true;
      clearInterval(active.timer);
      for (const socket of active.sockets) socket.terminate();
      await active.busy;
    }
    await this.collect(id);
    const step = await this.get(id);
    if (step.state !== "observing") return step;
    const snapshot = await this.snapshot(
      step.environmentId,
      step.subjectRef,
      Object.keys(step.startCursors) as SourceKind[],
    );
    const watermarks: Record<string, number> = {};
    for (const source of snapshot.observations)
      if (source.cursor !== undefined)
        watermarks[source.source] = source.cursor;
    await this.collect(id, watermarks);
    const collected = await this.get(id);
    step.events = collected.events;
    step.endCursors = collected.endCursors;
    step.gaps = collected.gaps;
    step.coverage = collected.coverage;
    for (const [source, watermark] of Object.entries(watermarks))
      if (source !== "renderer" && step.endCursors[source] !== watermark) {
        if (!step.optionalSources.includes(source as SourceKind))
          step.coverage = "partial";
        step.gaps.push(`${source}:unconfirmedEndWatermark`);
      }
    for (const source of snapshot.observations)
      if (
        source.status !== "available" ||
        `${source.generation}:${source.instance}` !==
          step.generations[source.source]
      ) {
        if (!step.optionalSources.includes(source.source))
          step.coverage = "partial";
        step.gaps.push(`${source.source}:sourceChanged`);
      }
    step.endSnapshotId = snapshot.stateSnapshotId;
    step.endedAt = new Date().toISOString();
    step.state = "ended";
    await this.save(step);
    this.active.delete(id);
    await this.evidence.attach({
      verificationId: step.verificationId,
      environmentId: step.environmentId,
      stepId: step.stepId,
      kind: "events",
      source: "step.events",
      mediaType: "application/json",
      coverage: step.coverage,
      bytes: Buffer.from(JSON.stringify(step)),
    });
    await this.evidence.attach({
      verificationId: step.verificationId,
      environmentId: step.environmentId,
      stepId: step.stepId,
      kind: "snapshot",
      source: "step.end",
      mediaType: "application/json",
      coverage: step.coverage,
      bytes: Buffer.from(JSON.stringify(snapshot)),
    });
    return step;
  }
  async list(verificationId: string, cursor = 0, limit = 50) {
    await this.records.read(verificationId);
    const items = [];
    for (const name of await this.storage.list("steps")) {
      const step = await this.storage.read(`steps/${name}`, stepSchema);
      if (step?.verificationId === verificationId)
        items.push({
          ...step,
          events: undefined,
          eventCount: step.events.length,
        });
    }
    items.sort((a, b) => a.startedAt.localeCompare(b.startedAt));
    return {
      items: items.slice(cursor, cursor + limit),
      nextCursor: Math.min(items.length, cursor + limit),
      hasMore: cursor + limit < items.length,
    };
  }
  async events(
    id: string,
    cursor?: string,
    limit = 100,
    filter: z.input<typeof eventFilterSchema> = {},
  ) {
    const step = await this.get(id);
    const environment = await this.environments.get(step.environmentId);
    if (
      step.dataGeneration !==
      String(environment.components.dataGeneration ?? "initial")
    )
      throw new VerificationError(
        problem(
          "staleReference",
          "The observation belongs to an earlier dataset generation; read its archived evidence instead",
          "reobserve",
        ),
      );
    return pageEvents(step, filter, cursor, limit);
  }
  async close(): Promise<void> {
    for (const id of this.active.keys())
      await this.end(id).catch(() => undefined);
  }
}
