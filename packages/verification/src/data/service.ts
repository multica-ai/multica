import { mkdir, writeFile, readFile } from "node:fs/promises";
import { join } from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { z } from "zod";
import {
  jsonSchema,
  resourceIdSchema,
  toJson,
  type Json,
} from "../contracts/index.js";
import {
  dataSnapshotSchema,
  datasetSchema,
  sourceSchema,
  type DataSnapshot,
  type Dataset,
  stepSchema,
} from "../contracts/resources.js";
import { canonicalJson, digest, type HostStorage } from "../host/storage.js";
import { problem, VerificationError } from "../host/problem.js";
import type { OperationContext } from "../host/jobs.js";
import type { Environments } from "../setup/environments.js";
import { contentManifest } from "./manifest.js";
import { exportWorkspace, workspaceContentSchema } from "./workspace.js";
import { exportSQLite } from "./sqlite-source.js";

const turnSchema = z
  .object({
    id: z.string(),
    turn_index: z.number().int().nonnegative(),
    status: z.string(),
    messages: z.array(
      z
        .object({
          id: z.string(),
          attachment_ids: z.array(z.string()).nullable().optional(),
        })
        .passthrough(),
    ),
    events: z.array(jsonSchema),
  })
  .passthrough();
const snapshotSchema = z
  .object({
    version: z.literal(2),
    store_generation: z.string(),
    snapshot_seq: z.number().int().nonnegative(),
    conversation: z
      .object({
        conversation_id: z.string(),
        provider: z.enum(["claude", "codex"]),
      })
      .passthrough(),
    turns: z.array(turnSchema),
    has_older_turns: z.boolean(),
    queue: z
      .object({
        items: z.array(jsonSchema),
        revision: z.number().int().nonnegative(),
        paused: z.boolean(),
      })
      .passthrough(),
  })
  .passthrough();
const attachmentSchema = z
  .object({
    originalId: z.string(),
    name: z.string(),
    mediaType: z.string(),
    data: z.string(),
    sha256: z.string(),
  })
  .strict();
const contentSchema = z
  .object({
    schemaVersion: z.literal(1),
    conversations: z.array(
      z
        .object({
          originalId: z.string(),
          snapshot: snapshotSchema,
          attachments: z.array(attachmentSchema),
        })
        .strict(),
    ),
  })
  .strict();
const maximumBytes = 60 * 1024 * 1024;
const assertSize = (bytes: number) => {
  if (bytes > maximumBytes)
    throw new VerificationError(
      problem(
        "coverageIncomplete",
        "The data snapshot exceeds 60 MB; select a smaller scope",
      ),
    );
};

export class DataService {
  private readonly updates = new Map<string, Promise<unknown>>();
  constructor(
    private readonly storage: HostStorage,
    private readonly environments: Environments,
  ) {}
  async bind(sourceRef: string) {
    const binding = this.environments.bindings.get(sourceRef);
    const source = sourceSchema.parse({
      schemaVersion: 1,
      sourceId: randomUUID(),
      bindingRef: sourceRef,
      kind: binding.kind,
      scope: binding.scope,
    });
    await this.storage.write(`sources/${source.sourceId}.json`, source);
    return source;
  }
  async source(sourceId: string) {
    const source = await this.storage.read(
      `sources/${resourceIdSchema.parse(sourceId)}.json`,
      sourceSchema,
    );
    if (!source)
      throw new VerificationError(
        problem("notFound", "The source is not bound"),
      );
    return source;
  }
  async inspect(sourceId: string) {
    const source = await this.source(sourceId);
    return {
      ...source,
      formats:
        source.kind === "daemon"
          ? ["multica.local-conversation/v1"]
          : ["multica.workspace/v1"],
      restorable: source.kind === "daemon" || source.kind === "backend",
      restorePhase: "onlineAfterServiceReady",
      limits:
        source.kind === "backend"
          ? [
              "Selected content only; no account or credential clone",
              "Execution source contexts, custom property definitions and project execution resources require a newer format",
            ]
          : ["Provider sessions and running execution are not resumed"],
      consistency: source.kind === "daemon" ? "perConversation" : "perEntity",
    };
  }
  async snapshot(
    sourceId: string,
    selection: {
      conversationIds?: string[];
      projectIds?: string[];
      issueIds?: string[];
    },
  ): Promise<DataSnapshot> {
    const captureStartedAt = new Date().toISOString();
    const source = await this.source(sourceId),
      bindings = this.environments.bindings,
      binding = bindings.get(source.bindingRef);
    let content: Json, format: DataSnapshot["format"];
    if (source.kind === "daemon" && binding.databaseFile) {
      content = await exportSQLite(binding, selection.conversationIds ?? []);
      format = "multica.local-conversation/v1";
    } else if (source.kind === "daemon") {
      const ids = [...new Set(selection.conversationIds ?? [])];
      if (!ids.length || ids.length > 100)
        throw new VerificationError(
          problem("invalidInput", "Select between 1 and 100 conversations"),
        );
      const conversations: z.infer<typeof contentSchema>["conversations"] = [];
      let bytes = 0;
      for (let position = 0; position < ids.length; position++) {
        if (ids.length > 100)
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "The conversation relationship closure exceeds 100 objects",
            ),
          );
        const id = ids[position]!,
          path = `/local/v2/sessions/${encodeURIComponent(id)}`;
        const initial = snapshotSchema.parse(
          await bindings.request(source.bindingRef, `${path}/snapshot`),
        );
        if (initial.conversation.side_source_id)
          throw new VerificationError(
            problem(
              "unsupportedCapability",
              "Side context copying requires a host-bound consistent SQLite export",
            ),
          );
        bytes += Buffer.byteLength(JSON.stringify(initial));
        assertSize(bytes);
        const turns = [...initial.turns];
        let older = initial.has_older_turns;
        while (older) {
          const before = turns[0]?.turn_index;
          if (before === undefined || turns.length >= 20000)
            throw new VerificationError(
              problem(
                "coverageIncomplete",
                "The history is incomplete or too large",
              ),
            );
          const page = z
            .object({
              store_generation: z.string(),
              turns: z.array(turnSchema),
              has_older_turns: z.boolean(),
            })
            .parse(
              await bindings.request(
                source.bindingRef,
                `${path}/history?before_index=${before}`,
              ),
            );
          if (
            page.store_generation !== initial.store_generation ||
            !page.turns.length ||
            page.turns.some((turn) => turn.turn_index >= before)
          )
            throw new VerificationError(
              problem(
                "coverageIncomplete",
                "The source history changed during export",
              ),
            );
          bytes += Buffer.byteLength(JSON.stringify(page.turns));
          assertSize(bytes);
          turns.unshift(...page.turns);
          older = page.has_older_turns;
        }
        for (const key of ["forked_from_id", "side_source_id"]) {
          const related =
            typeof initial[key] === "string"
              ? initial[key]
              : typeof initial.conversation[key] === "string"
                ? initial.conversation[key]
                : "";
          if (related && !ids.includes(related)) ids.push(related);
        }
        const attachmentIds = new Set<string>();
        const collect = (value: Json, key = "") => {
          if (
            typeof value === "string" &&
            [
              "attachment_id",
              "attachmentId",
              "attachment_ids",
              "attachmentIds",
            ].includes(key)
          )
            attachmentIds.add(value);
          else if (Array.isArray(value))
            value.forEach((item) => collect(item, key));
          else if (value && typeof value === "object")
            for (const [name, item] of Object.entries(value))
              collect(item, name);
        };
        collect(jsonSchema.parse({ turns, queue: initial.queue }));
        const attachments: z.infer<typeof attachmentSchema>[] = [];
        for (const attachmentId of attachmentIds) {
          const metadata = z
            .object({
              name: z.string(),
              mime_type: z.string(),
              size: z.number().int().nonnegative(),
            })
            .parse(
              await bindings.request(
                source.bindingRef,
                `/local/v2/attachments/${encodeURIComponent(attachmentId)}/metadata`,
              ),
            );
          if (metadata.size > 25 * 1024 * 1024)
            throw new VerificationError(
              problem(
                "coverageIncomplete",
                "An attachment exceeds its copy limit",
              ),
            );
          assertSize(bytes + Math.ceil((metadata.size * 4) / 3));
          const data = await bindings.bytes(
            source.bindingRef,
            `/local/v2/attachments/${encodeURIComponent(attachmentId)}`,
          );
          if (data.length !== metadata.size)
            throw new VerificationError(
              problem(
                "identityMismatch",
                "Attachment content does not match its metadata",
              ),
            );
          bytes += Math.ceil((data.length * 4) / 3);
          attachments.push({
            originalId: attachmentId,
            name: metadata.name,
            mediaType: metadata.mime_type,
            data: data.toString("base64"),
            sha256: createHash("sha256").update(data).digest("hex"),
          });
        }
        const final = snapshotSchema.parse(
          await bindings.request(source.bindingRef, `${path}/snapshot`),
        );
        if (
          final.store_generation !== initial.store_generation ||
          final.snapshot_seq !== initial.snapshot_seq ||
          final.queue.revision !== initial.queue.revision
        )
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "The conversation changed while being copied; retry when stable",
              "inspect",
            ),
          );
        conversations.push({
          originalId: id,
          snapshot: { ...initial, turns, has_older_turns: false },
          attachments,
        });
      }
      content = jsonSchema.parse({ schemaVersion: 1, conversations });
      format = "multica.local-conversation/v1";
    } else if (source.kind === "backend") {
      content = await exportWorkspace(bindings, source.bindingRef, selection);
      format = "multica.workspace/v1";
    } else
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "Select a daemon or backend data source",
        ),
      );
    return this.saveSnapshot(sourceId, format, content, true, captureStartedAt);
  }
  private async saveSnapshot(
    sourceId: string,
    format: DataSnapshot["format"],
    content: Json,
    complete: boolean,
    captureStartedAt = new Date().toISOString(),
  ) {
    assertSize(Buffer.byteLength(canonicalJson(content)));
    const source = await this.source(sourceId);
    const binding = this.environments.bindings.get(source.bindingRef);
    const snapshot = dataSnapshotSchema.parse({
      schemaVersion: 1,
      dataSnapshotId: randomUUID(),
      sourceId,
      format,
      capturedAt: new Date().toISOString(),
      captureStartedAt,
      sourceVersion: binding.identity.commit ?? null,
      sourceScope: source.scope,
      capabilities: [format, "boundedSnapshot", "attachmentChecksums"],
      ...contentManifest(content),
      consistency:
        format === "multica.local-conversation/v1"
          ? "perConversation"
          : "perEntity",
      content,
      sha256: digest(canonicalJson(content)),
      complete,
    });
    for (const attachment of snapshot.attachments) {
      const pointer = attachment.relativePath.split("/").slice(1);
      let value: Json = snapshot.content;
      for (const segment of pointer)
        value = Array.isArray(value)
          ? value[Number(segment)]!
          : (value as Record<string, Json>)[segment]!;
      const bytes = Buffer.from(
        z.object({ data: z.string() }).parse(value).data,
        "base64",
      );
      const directory = `data-snapshots/${snapshot.dataSnapshotId}.files`;
      await mkdir(join(this.storage.root, directory), {
        recursive: true,
        mode: 0o700,
      });
      attachment.relativePath = `${directory}/${digest(attachment.originalId)}`;
      await writeFile(join(this.storage.root, attachment.relativePath), bytes, {
        mode: 0o600,
        flag: "wx",
      });
    }
    await this.storage.write(
      `data-snapshots/${snapshot.dataSnapshotId}.json`,
      snapshot,
    );
    return snapshot;
  }
  async getSnapshot(id: string) {
    const snapshot = await this.storage.read(
      `data-snapshots/${resourceIdSchema.parse(id)}.json`,
      dataSnapshotSchema,
    );
    if (!snapshot)
      throw new VerificationError(
        problem("notFound", "The data snapshot does not exist"),
      );
    if (digest(canonicalJson(snapshot.content)) !== snapshot.sha256)
      throw new VerificationError(
        problem("identityMismatch", "The data snapshot checksum changed"),
      );
    for (const attachment of snapshot.attachments) {
      const expected = `data-snapshots/${snapshot.dataSnapshotId}.files/${digest(attachment.originalId)}`;
      if (attachment.relativePath !== expected)
        throw new VerificationError(
          problem("identityMismatch", "The snapshot attachment path changed"),
        );
      const bytes = await readFile(join(this.storage.root, expected));
      if (
        bytes.length !== attachment.bytes ||
        createHash("sha256").update(bytes).digest("hex") !== attachment.sha256
      )
        throw new VerificationError(
          problem("identityMismatch", "The snapshot attachment bytes changed"),
        );
    }
    return snapshot;
  }
  async describeSnapshot(
    id: string,
    section: "entities" | "relationships" | "attachments" = "entities",
    cursor = 0,
    limit = 50,
  ) {
    z.number().int().nonnegative().parse(cursor);
    z.number().int().min(1).max(100).parse(limit);
    const snapshot = await this.getSnapshot(id);
    const {
      content: _,
      entities,
      relationships,
      attachments,
      ...metadata
    } = snapshot;
    const items = { entities, relationships, attachments }[section];
    return {
      ...metadata,
      counts: {
        entities: entities.length,
        relationships: relationships.length,
        attachments: attachments.length,
      },
      section,
      items: items.slice(cursor, cursor + limit),
      nextCursor: Math.min(items.length, cursor + limit),
      hasMore: cursor + limit < items.length,
    };
  }
  async get(id: string): Promise<Dataset> {
    const dataset = await this.storage.read(
      `datasets/${resourceIdSchema.parse(id)}.json`,
      datasetSchema,
    );
    if (!dataset)
      throw new VerificationError(
        problem("notFound", "The dataset does not exist"),
      );
    return dataset;
  }
  async live(environmentId: string, sourceId: string): Promise<Dataset> {
    const environment = await this.environments.get(environmentId),
      source = await this.source(sourceId);
    if (
      environment.dataMode !== "liveReadOnly" ||
      environment.apiOrigin !== source.scope.apiOrigin ||
      environment.sourceRef !== source.bindingRef
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The live source does not match its reference environment",
        ),
      );
    const dataset = datasetSchema.parse({
      schemaVersion: 1,
      datasetId: randomUUID(),
      environmentId,
      mode: "liveReadOnly",
      sourceId,
      restoreVersion: 1,
      refs: {},
      resources: [],
      state: "ready",
    });
    await this.storage.write(`datasets/${dataset.datasetId}.json`, dataset);
    return dataset;
  }
  async restore(
    environmentId: string,
    dataSnapshotId: string,
    existing?: Dataset,
    context?: OperationContext,
    resume = false,
  ): Promise<Dataset> {
    const environment = await this.environments.get(environmentId);
    if (environment.dataMode === "liveReadOnly")
      throw new VerificationError(
        problem("forbidden", "Snapshots cannot be imported into live data"),
      );
    const snapshot = await this.getSnapshot(dataSnapshotId);
    if (snapshot.format === "multica.workspace/v1" && snapshot.complete)
      return this.restoreWorkspace(
        environmentId,
        snapshot,
        existing,
        context,
        resume,
      );
    if (
      snapshot.format !== "multica.local-conversation/v1" ||
      !snapshot.complete
    )
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "Only complete conversation snapshots can be restored; workspace samples are read-only reference evidence",
        ),
      );
    if (!resume) await this.advanceDataGeneration(environmentId);
    const content = contentSchema.parse(snapshot.content),
      datasetId = existing?.datasetId ?? randomUUID();
    const restoreVersion =
      resume && existing
        ? existing.restoreVersion
        : (existing?.restoreVersion ?? 0) + 1;
    const ref = await this.environments.connection(environmentId, "daemon");
    this.environments.bindings.assertTest(ref);
    const dataset = datasetSchema.parse({
      schemaVersion: 1,
      datasetId,
      environmentId,
      mode: existing?.mode ?? "realSnapshot",
      providerScript: "controlled/v1",
      dataSnapshotId,
      restoreVersion,
      refs: {},
      resources: [],
      state: "partial",
    });
    await context?.addResource(datasetId);
    await this.storage.write(`datasets/${datasetId}.json`, dataset);
    const result = z
      .object({
        schemaVersion: z.literal(1),
        mapping: z.record(z.string(), z.string()),
        policy: z.string(),
        transformations: z.array(z.string()),
      })
      .parse(
        await this.environments.bindings.request(
          ref,
          "/local/v2/verification/import",
          "POST",
          { ...content, restoreId: `${datasetId}:${restoreVersion}` },
        ),
      );
    dataset.transformations = result.transformations;
    dataset.refs = result.mapping;
    dataset.resources = content.conversations.map((item) =>
      z.string().min(1).parse(result.mapping[item.originalId]),
    );
    dataset.refs.conversationId = dataset.resources[0]!;
    dataset.state = "ready";
    await this.storage.write(`datasets/${datasetId}.json`, dataset);
    return dataset;
  }
  private async restoreWorkspace(
    environmentId: string,
    snapshot: DataSnapshot,
    existing?: Dataset,
    context?: OperationContext,
    resume = false,
  ): Promise<Dataset> {
    const content = workspaceContentSchema.parse(snapshot.content);
    const ref = await this.environments.connection(environmentId, "backend");
    const binding = this.environments.bindings.assertTest(ref);
    const capability = z
      .object({
        environmentId: z.string(),
        format: z.literal("multica.workspace/v1"),
        restorePhase: z.literal("onlineAfterServiceReady"),
      })
      .parse(
        await this.environments.bindings.request(
          ref,
          "/api/verification/capabilities",
        ),
      );
    if (
      capability.environmentId !== environmentId ||
      binding.scope.apiOrigin !==
        (await this.environments.get(environmentId)).apiOrigin
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "Workspace import capability does not belong to the target environment",
        ),
      );
    if (!resume) await this.advanceDataGeneration(environmentId);
    const dataset = datasetSchema.parse({
      schemaVersion: 1,
      datasetId: existing?.datasetId ?? randomUUID(),
      environmentId,
      mode: existing?.mode ?? "realSnapshot",
      dataSnapshotId: snapshot.dataSnapshotId,
      restoreVersion:
        resume && existing
          ? existing.restoreVersion
          : (existing?.restoreVersion ?? 0) + 1,
      refs: {},
      resources: [],
      state: "partial",
    });
    await context?.addResource(dataset.datasetId);
    await this.storage.write(`datasets/${dataset.datasetId}.json`, dataset);
    const result = z
      .object({
        schemaVersion: z.literal(1),
        mapping: z.record(z.string(), z.string()),
        transformations: z.array(z.string()),
      })
      .parse(
        await this.environments.bindings.request(
          ref,
          "/api/verification/import",
          "POST",
          {
            restoreId: `${dataset.datasetId}:${dataset.restoreVersion}`,
            content,
          },
        ),
      );
    dataset.refs = result.mapping;
    dataset.refs.workspaceId = binding.scope.workspaceId;
    if (content.projects[0])
      dataset.refs.projectId = z
        .string()
        .min(1)
        .parse(result.mapping[content.projects[0].id]);
    if (content.issues[0])
      dataset.refs.issueId = z
        .string()
        .min(1)
        .parse(result.mapping[content.issues[0].id]);
    dataset.resources = [...content.projects, ...content.issues].map((item) =>
      z.string().min(1).parse(result.mapping[item.id]),
    );
    dataset.transformations = result.transformations;
    dataset.state = "ready";
    await this.storage.write(`datasets/${dataset.datasetId}.json`, dataset);
    return dataset;
  }
  resume(datasetId: string, context?: OperationContext): Promise<Dataset> {
    return this.serialize(datasetId, async () => {
      const dataset = await this.get(datasetId);
      if (dataset.state !== "partial" || !dataset.dataSnapshotId)
        throw new VerificationError(
          problem("invalidInput", "Only a partial import can be reconciled"),
        );
      return this.restore(
        dataset.environmentId,
        dataset.dataSnapshotId,
        dataset,
        context,
        true,
      );
    });
  }
  reset(datasetId: string): Promise<Dataset> {
    return this.serialize(datasetId, async () => {
      const dataset = await this.get(datasetId);
      if (dataset.mode === "liveReadOnly" || !dataset.dataSnapshotId)
        throw new VerificationError(
          problem("forbidden", "Only an owned snapshot copy can be reset"),
        );
      const snapshot = await this.getSnapshot(dataset.dataSnapshotId);
      await this.advanceDataGeneration(dataset.environmentId);
      if (snapshot.format === "multica.workspace/v1") {
        const ref = await this.environments.connection(
          dataset.environmentId,
          "backend",
        );
        this.environments.bindings.assertTest(ref);
        await this.environments.bindings.request(
          ref,
          "/api/verification/reset",
          "POST",
          { restoreId: `${dataset.datasetId}:${dataset.restoreVersion}` },
        );
        return this.restore(
          dataset.environmentId,
          dataset.dataSnapshotId,
          dataset,
        );
      }
      if (snapshot.format !== "multica.local-conversation/v1")
        throw new VerificationError(
          problem(
            "unsupportedCapability",
            "Only owned local conversation copies can be reset",
          ),
        );
      const ref = await this.environments.connection(
        dataset.environmentId,
        "daemon",
      );
      this.environments.bindings.assertTest(ref);
      for (const id of dataset.resources) {
        await this.environments.bindings.request(
          ref,
          `/local/v2/sessions/${encodeURIComponent(id)}/commands`,
          "POST",
          { type: "interrupt" },
        );
        await this.environments.bindings.request(
          ref,
          `/local/v2/sessions/${encodeURIComponent(id)}/commands`,
          "POST",
          { type: "update", archived: true },
        );
      }
      const restored = await this.restore(
        dataset.environmentId,
        dataset.dataSnapshotId,
        dataset,
      );
      if (dataset.mode === "fixture")
        await this.environments.bindings.request(
          ref,
          `/local/v2/sessions/${encodeURIComponent(restored.refs.conversationId!)}/commands`,
          "POST",
          { type: "queue_update", action: "resume" },
        );
      return restored;
    });
  }
  async workspaceFixture(
    environmentId: string,
    issueCount = 3,
    context?: OperationContext,
  ): Promise<Dataset> {
    z.number().int().min(0).max(20).parse(issueCount);
    const ref = await this.environments.connection(environmentId, "backend");
    const binding = this.environments.bindings.assertTest(ref);
    const { sourceId } = await this.bind(ref);
    const catalog = z
      .object({
        statuses: z.array(
          z
            .object({
              key: z.string(),
              name: z.string(),
              description: z.string(),
              category: z.string(),
              color: z.string(),
              icon: z.string(),
              position: z.number(),
            })
            .passthrough(),
        ),
      })
      .parse(
        await this.environments.bindings.request(ref, "/api/issue-statuses"),
      );
    const id = (key: string) => {
      const hex = digest(`workspace.basic/v1:${key}`);
      return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-4${hex.slice(13, 16)}-8${hex.slice(17, 20)}-${hex.slice(20, 32)}`;
    };
    const keys = [
      "backlog",
      "todo",
      "in_progress",
      "in_review",
      "done",
      "blocked",
      "cancelled",
    ];
    const statuses = keys.map((key) => {
      const status = catalog.statuses.find((item) => item.key === key);
      if (!status)
        throw new VerificationError(
          problem(
            "unsupportedCapability",
            `The target catalog cannot create workspace.basic/v1: ${key} is missing`,
          ),
        );
      return {
        id: id(`status:${key}`),
        key,
        name: status.name,
        description: status.description,
        category: status.category,
        color: status.color,
        icon: status.icon,
        position: status.position,
      };
    });
    const projectId = id("project"),
      workflowId = id("workflow"),
      workspaceId = id("workspace");
    const snapshot = await this.saveSnapshot(
      sourceId,
      "multica.workspace/v1",
      toJson({
        schemaVersion: 1,
        sourceOrigin: "http://fixture.invalid",
        workspace: {
          id: workspaceId,
          name: "Verification workspace",
          slug: "verification",
        },
        statuses,
        workflows: [
          {
            id: workflowId,
            name: "Verification workflow",
            description: "Controlled workspace fixture",
            steps: keys.map((status_key) => ({
              status_key,
              handler: { type: "none" },
              instructions: "",
            })),
          },
        ],
        projects: [
          {
            id: projectId,
            title: "Verification project",
            status: "planned",
            priority: "none",
            workflow_id: workflowId,
          },
        ],
        issues: Array.from({ length: issueCount }, (_, index) => ({
          id: id(`issue:${index}`),
          project_id: projectId,
          title: `Verification issue ${index + 1}`,
          description: "Controlled workspace content",
          status: index === 0 ? "todo" : "backlog",
          priority: "none",
        })),
        comments: [],
        labels: [],
        attachments: [],
      }),
      true,
    );
    const dataset = await this.restoreWorkspace(
      environmentId,
      snapshot,
      undefined,
      context,
    );
    dataset.mode = "fixture";
    dataset.refs.workspaceId = binding.scope.workspaceId;
    await this.storage.write(`datasets/${dataset.datasetId}.json`, dataset);
    return dataset;
  }
  async fixture(
    environmentId: string,
    historyMessageCount = 0,
    context?: OperationContext,
  ): Promise<Dataset> {
    z.number().int().min(0).max(1000).parse(historyMessageCount);
    const bindingRef = await this.environments.connection(
      environmentId,
      "daemon",
    );
    this.environments.bindings.assertTest(bindingRef);
    const { sourceId } = await this.bind(bindingRef),
      conversationId = "fixture-conversation",
      turns = [];
    for (let i = 0; i < historyMessageCount; i++)
      turns.push({
        id: `fixture-turn-${i}`,
        turn_index: i,
        request_id: `fixture-request-${i}`,
        status: "completed",
        started_at: "2026-01-01T00:00:00Z",
        completed_at: "2026-01-01T00:00:00Z",
        messages: [
          {
            id: `fixture-message-${i}`,
            role: "user",
            content: `Verification history ${i + 1}`,
            attachment_ids: [],
            created_at: "2026-01-01T00:00:00Z",
          },
        ],
        events: [],
      });
    const content = jsonSchema.parse({
      schemaVersion: 1,
      conversations: [
        {
          originalId: conversationId,
          snapshot: {
            version: 2,
            store_generation: "fixture",
            snapshot_seq: 0,
            conversation: {
              conversation_id: conversationId,
              provider: "claude",
              title: "Controlled verification",
            },
            turns,
            has_older_turns: false,
            queue: { revision: 0, paused: false, items: [] },
          },
          attachments: [],
        },
      ],
    });
    const snapshot = await this.saveSnapshot(
      sourceId,
      "multica.local-conversation/v1",
      content,
      true,
    );
    const dataset = await this.restore(
      environmentId,
      snapshot.dataSnapshotId,
      undefined,
      context,
    );
    dataset.mode = "fixture";
    await this.storage.write(`datasets/${dataset.datasetId}.json`, dataset);
    await this.environments.bindings.request(
      bindingRef,
      `/local/v2/sessions/${encodeURIComponent(dataset.refs.conversationId!)}/commands`,
      "POST",
      { type: "queue_update", action: "resume" },
    );
    return dataset;
  }
  async control(
    datasetId: string,
    action: "inspect" | "release" | "chunk" | "fail",
    text = "",
    expectedRequestId?: string,
  ): Promise<unknown> {
    const dataset = await this.get(datasetId);
    if (
      dataset.mode === "liveReadOnly" ||
      !dataset.refs.conversationId ||
      dataset.state !== "ready"
    )
      throw new VerificationError(
        problem(
          "forbidden",
          "Fixture control requires a ready owned test conversation",
        ),
      );
    if (action !== "inspect" && !expectedRequestId)
      throw new VerificationError(
        problem(
          "invalidInput",
          "Mutating fixture control requires the original expectedRequestId",
        ),
      );
    const ref = await this.environments.connection(
      dataset.environmentId,
      "daemon",
    );
    this.environments.bindings.assertTest(ref);
    return action === "inspect"
      ? this.environments.bindings.request(
          ref,
          `/local/v2/verification/fixture?conversationId=${encodeURIComponent(dataset.refs.conversationId)}`,
        )
      : this.environments.bindings.request(
          ref,
          "/local/v2/verification/fixture",
          "POST",
          {
            conversationId: dataset.refs.conversationId,
            action,
            text,
            expectedRequestId,
          },
        );
  }
  private async advanceDataGeneration(environmentId: string): Promise<void> {
    for (const name of await this.storage.list("steps")) {
      const step = await this.storage.read(`steps/${name}`, stepSchema);
      if (step?.environmentId === environmentId && step.state === "observing")
        throw new VerificationError(
          problem(
            "instanceBusy",
            "End active observation steps before replacing the dataset",
            "inspect",
            [step.stepId],
          ),
        );
    }
    await this.environments.advanceDataGeneration(environmentId);
  }
  private serialize<T>(id: string, run: () => Promise<T>): Promise<T> {
    const result = (this.updates.get(id) ?? Promise.resolve())
      .catch(() => undefined)
      .then(run);
    this.updates.set(id, result);
    void result
      .finally(() => {
        if (this.updates.get(id) === result) this.updates.delete(id);
      })
      .catch(() => undefined);
    return result;
  }
}
