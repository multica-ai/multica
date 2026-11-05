import { randomUUID, createHash } from "node:crypto";
import {
  mkdir,
  readFile,
  realpath,
  stat,
  writeFile,
  rm,
} from "node:fs/promises";
import {
  dirname,
  basename,
  extname,
  isAbsolute,
  join,
  relative,
} from "node:path";
import { z } from "zod";
import {
  checkSchema,
  datasetSchema,
  environmentSchema,
  evidenceSchema,
  stepSchema,
  stateSnapshotSchema,
  dataSnapshotSchema,
  type Evidence,
} from "../contracts/resources.js";
import {
  jsonSchema,
  resourceIdSchema,
  verificationSchema,
  type Json,
} from "../contracts/index.js";
import type { HostStorage } from "../host/storage.js";
import type { VerificationRecords } from "./records.js";
import { redactLogText } from "../host/redact.js";
import { problem, VerificationError } from "../host/problem.js";

import { manifestSchema, verifyBuildFiles } from "../setup/build-cache.js";
import { sourceIdentity } from "../setup/source-identity.js";
import { enforcePlan } from "./requirements.js";

const reportSchema = z.object({
  schemaVersion: z.literal(1),
  reportRevision: resourceIdSchema,
  verificationId: resourceIdSchema,
  conclusion: z.enum(["passed", "failed", "inconclusive", "waived"]),
  explanation: z.string(),
  uncovered: z.array(z.string()),
  waiverRef: z.string().optional(),
  createdAt: z.string(),
  evidence: z.array(evidenceSchema),
  checks: z.array(checkSchema),
  cleanup: jsonSchema,
  plan: verificationSchema.shape.plan,
  environments: z.array(environmentSchema),
  datasets: z.array(datasetSchema),
  dataSnapshots: z.array(dataSnapshotSchema.omit({ content: true })),
  steps: z.array(
    stepSchema.omit({ events: true }).extend({ eventCount: z.number() }),
  ),
  jobs: z.array(jsonSchema),
});
export type Report = z.infer<typeof reportSchema>;
const sha256 = (bytes: Uint8Array) =>
  createHash("sha256").update(bytes).digest("hex");
const escape = (text: string) =>
  text.replace(
    /[&<>"']/g,
    (character) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        character
      ]!,
  );

export class EvidenceStore {
  private writes: Promise<unknown> = Promise.resolve();
  constructor(
    private readonly storage: HostStorage,
    private readonly records: VerificationRecords,
    private readonly attachmentRoots: string[] = [],
    private readonly waivers: ReadonlySet<string> = new Set(),
    private readonly protectedFiles: string[] = [],
  ) {}
  async attach(input: {
    verificationId: string;
    environmentId?: string;
    stepId?: string;
    kind: Evidence["kind"];
    source: string;
    mediaType: string;
    capturedAt?: string;
    colorScheme?: "light" | "dark";
    coverage?: Evidence["coverage"];
    bytes: Uint8Array;
  }): Promise<Evidence> {
    const record = await this.records.read(input.verificationId);
    if (record.state === "pruned")
      throw new VerificationError(
        problem(
          "notFound",
          "The verification archive expired under its retention policy",
        ),
      );
    if (input.bytes.length > 32 * 1024 * 1024)
      throw new VerificationError(
        problem("invalidInput", "Evidence exceeds the 32 MB file limit"),
      );
    if (input.environmentId) {
      const environment = await this.storage.read(
        `environments/${resourceIdSchema.parse(input.environmentId)}.json`,
        environmentSchema,
      );
      if (!environment || environment.projectRef !== this.storage.projectRef)
        throw new VerificationError(
          problem(
            "forbidden",
            "Evidence environment is not owned by this project",
          ),
        );
    }
    if (input.stepId) {
      const step = await this.storage.read(
        `steps/${resourceIdSchema.parse(input.stepId)}.json`,
        stepSchema,
      );
      if (
        !step ||
        step.verificationId !== input.verificationId ||
        (input.environmentId && step.environmentId !== input.environmentId)
      )
        throw new VerificationError(
          problem(
            "identityMismatch",
            "Evidence step and verification do not match",
          ),
        );
    }
    if (input.kind === "screenshot" && !isImage(input.bytes, input.mediaType))
      throw new VerificationError(
        problem(
          "invalidInput",
          "The screenshot does not contain a supported image",
        ),
      );
    const evidenceId = randomUUID(),
      extension =
        input.mediaType === "image/png"
          ? ".png"
          : input.mediaType === "image/jpeg"
            ? ".jpg"
            : input.mediaType === "application/json"
              ? ".json"
              : ".txt";
    const path = `artifacts/${input.verificationId}/${evidenceId}${extension}`;
    await mkdir(dirname(join(this.storage.root, path)), {
      recursive: true,
      mode: 0o700,
    });
    await writeFile(join(this.storage.root, path), input.bytes, {
      mode: 0o600,
      flag: "wx",
    });
    const value = evidenceSchema.parse({
      schemaVersion: 1,
      evidenceId,
      verificationId: input.verificationId,
      environmentId: input.environmentId,
      stepId: input.stepId,
      kind: input.kind,
      source: input.source,
      path,
      sha256: sha256(input.bytes),
      bytes: input.bytes.length,
      mediaType: input.mediaType,
      colorScheme: input.colorScheme,
      capturedAt: input.capturedAt ?? new Date().toISOString(),
      receivedAt: new Date().toISOString(),
      coverage: input.coverage ?? "unknown",
    });
    await this.storage.write(`evidence/${evidenceId}.json`, value);
    return value;
  }
  async attachImage(input: {
    verificationId: string;
    environmentId: string;
    stepId: string;
    data: string;
    mediaType: "image/png" | "image/jpeg";
    source: string;
    capturedAt: string;
    colorScheme: "light" | "dark";
  }): Promise<Evidence> {
    const bytes = Buffer.from(input.data, "base64");
    if (bytes.toString("base64") !== input.data)
      throw new VerificationError(
        problem("invalidInput", "Image data must be canonical base64"),
      );
    return this.attach({
      ...input,
      kind: "screenshot",
      coverage: "complete",
      bytes,
    });
  }
  async attachFile(
    input: Omit<
      Parameters<EvidenceStore["attach"]>[0],
      "bytes" | "mediaType"
    > & { path: string; mediaType?: string },
  ): Promise<Evidence> {
    const path = await realpath(input.path);
    const hostRelative = relative(this.storage.root, path);
    const protectedFiles = await Promise.all(
      this.protectedFiles.map((file) => realpath(file).catch(() => null)),
    );
    if (
      (!hostRelative.startsWith("..") && !isAbsolute(hostRelative)) ||
      protectedFiles.includes(path)
    )
      throw new VerificationError(
        problem(
          "forbidden",
          "Private host state and source credential/database files cannot be attached; use the scoped tools",
        ),
      );
    if (
      path
        .split(/[\\/]/)
        .some(
          (part) =>
            part.startsWith(".env") ||
            [".npmrc", ".netrc", ".git-credentials"].includes(part) ||
            /\.(token|pem|key)$/.test(part),
        )
    )
      throw new VerificationError(
        problem("forbidden", "Credential files cannot be attached as evidence"),
      );
    const roots = await Promise.all(
      [this.storage.projectRoot, ...this.attachmentRoots].map((root) =>
        realpath(root),
      ),
    );
    if (
      !roots.some((root) => {
        const rel = relative(root, path);
        return !rel.startsWith("..") && !isAbsolute(rel);
      })
    )
      throw new VerificationError(
        problem(
          "forbidden",
          "The evidence file is outside the host-authorized roots",
        ),
      );
    const info = await stat(path);
    if (!info.isFile() || info.size > 32 * 1024 * 1024)
      throw new VerificationError(
        problem("invalidInput", "Invalid evidence file"),
      );
    const mediaType =
      input.mediaType ??
      {
        ".png": "image/png",
        ".jpg": "image/jpeg",
        ".jpeg": "image/jpeg",
        ".json": "application/json",
      }[extname(path).toLowerCase()] ??
      "text/plain";
    if (
      input.kind === "screenshot" &&
      (!input.environmentId ||
        !input.stepId ||
        !input.capturedAt ||
        !input.colorScheme)
    )
      throw new VerificationError(
        problem(
          "invalidInput",
          "External screenshots require environment, step, capturedAt and colorScheme provenance",
        ),
      );
    return this.attach({
      ...input,
      mediaType,
      coverage: input.kind === "screenshot" ? "complete" : input.coverage,
      bytes:
        input.kind === "screenshot"
          ? await readFile(path)
          : Buffer.from(redactLogText(await readFile(path, "utf8"))),
    });
  }
  async get(id: string): Promise<Evidence> {
    const value = await this.storage.read(
      `evidence/${resourceIdSchema.parse(id)}.json`,
      evidenceSchema,
    );
    if (!value)
      throw new VerificationError(
        problem("notFound", "Evidence does not exist"),
      );
    return value;
  }
  async read(id: string): Promise<{ evidence: Evidence; bytes: Buffer }> {
    const evidence = await this.get(id),
      path = await realpath(join(this.storage.root, evidence.path)),
      rel = relative(this.storage.root, path);
    if (rel.startsWith("..") || isAbsolute(rel))
      throw new VerificationError(
        problem("identityMismatch", "The evidence path escaped its archive"),
      );
    const bytes = await readFile(path);
    if (bytes.length !== evidence.bytes || sha256(bytes) !== evidence.sha256)
      throw new VerificationError(
        problem(
          "identityMismatch",
          "Evidence is missing or its checksum changed",
          "inspect",
          [id],
        ),
      );
    return { evidence, bytes };
  }
  async list(
    verificationId: string,
    cursor = 0,
    limit = 50,
  ): Promise<{ items: Evidence[]; nextCursor: number; hasMore: boolean }> {
    await this.records.read(verificationId);
    const all: Evidence[] = [];
    for (const name of await this.storage.list("evidence")) {
      const value = await this.storage.read(`evidence/${name}`, evidenceSchema);
      if (value?.verificationId === verificationId) all.push(value);
    }
    all.sort(
      (a, b) =>
        a.receivedAt.localeCompare(b.receivedAt) ||
        a.evidenceId.localeCompare(b.evidenceId),
    );
    return {
      items: all.slice(cursor, cursor + limit),
      nextCursor: Math.min(all.length, cursor + limit),
      hasMore: cursor + limit < all.length,
    };
  }
  bindEnvironment(
    verificationId: string,
    environmentId: string,
    role: "baseline" | "candidate" | "smoke",
  ): Promise<Json> {
    return this.serialize(async () => {
      const record = await this.records.read(verificationId),
        environment = await this.storage.read(
          `environments/${resourceIdSchema.parse(environmentId)}.json`,
          environmentSchema,
        );
      if (!environment || environment.projectRef !== this.storage.projectRef)
        throw new VerificationError(
          problem("forbidden", "The environment is outside this project"),
        );
      const prior = record.environments.find(
        (entry) => entry.environmentId === environmentId,
      );
      if (prior && prior.role !== role)
        throw new VerificationError(
          problem(
            "requestConflict",
            "An environment cannot change comparison role",
          ),
        );
      if (!prior) record.environments.push({ environmentId, role });
      await this.storage.write(
        `verifications/${verificationId}.json`,
        verificationSchema.parse(record),
      );
      return jsonSchema.parse(record);
    });
  }
  finalize(input: {
    verificationId: string;
    conclusion: Report["conclusion"];
    explanation: string;
    uncovered: string[];
    waiverRef?: string;
  }): Promise<Report> {
    return this.serialize(async () => {
      const record = await this.records.read(input.verificationId),
        evidence: Evidence[] = [],
        checks: z.infer<typeof checkSchema>[] = [];
      let cursor = 0,
        more = true;
      while (more) {
        const page = await this.list(input.verificationId, cursor, 100);
        evidence.push(...page.items);
        cursor = page.nextCursor;
        more = page.hasMore;
      }
      for (const item of evidence) await this.read(item.evidenceId);
      for (const name of await this.storage.list("checks")) {
        const check = await this.storage.read(`checks/${name}`, checkSchema);
        if (check?.verificationId === record.verificationId) checks.push(check);
      }
      if (
        input.conclusion === "waived" &&
        (!input.waiverRef || !this.waivers.has(input.waiverRef))
      )
        throw new VerificationError(
          problem(
            "forbidden",
            "A waiver must be supplied by the host's explicit user context",
          ),
        );
      if (input.conclusion === "passed") {
        const datasets = [],
          steps = [];
        for (const name of await this.storage.list("datasets")) {
          const value = await this.storage.read(
            `datasets/${name}`,
            datasetSchema,
          );
          if (value) datasets.push(value);
        }
        for (const name of await this.storage.list("steps")) {
          const value = await this.storage.read(`steps/${name}`, stepSchema);
          if (value?.verificationId === record.verificationId)
            steps.push(value);
        }
        for (const entry of record.environments) {
          const environment = await this.storage.read(
            `environments/${entry.environmentId}.json`,
            environmentSchema,
          );
          if (
            environment?.components.sourceTreeDigest &&
            environment.components.sourceTreeDigest !==
              (await sourceIdentity(environment.checkout))
          )
            throw new VerificationError(
              problem(
                "coverageIncomplete",
                "The source tree changed after preparation; the checks no longer identify that build",
              ),
            );
        }
        for (const entry of record.environments) {
          const environment = await this.storage.read(
            `environments/${entry.environmentId}.json`,
            environmentSchema,
          );
          if (environment?.components.build)
            await verifyBuildFiles(
              environment.checkout,
              z
                .object({ manifest: manifestSchema })
                .parse(environment.components.build).manifest,
            );
        }
        enforcePlan(record, checks, datasets, steps, evidence);
        const candidateIds = new Set(
            record.environments
              .filter((entry) => entry.role !== "baseline")
              .map((entry) => entry.environmentId),
          ),
          baselineIds = new Set(
            record.environments
              .filter((entry) => entry.role === "baseline")
              .map((entry) => entry.environmentId),
          );
        const requiredChecks = checks.filter((check) =>
          candidateIds.has(check.environmentId),
        );
        if (
          input.uncovered.length ||
          !requiredChecks.length ||
          requiredChecks.some((check) => check.status !== "passed") ||
          evidence.some(
            (item) => item.kind === "check" && item.coverage !== "complete",
          )
        )
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "Passing requires completed checks with complete evidence and no required uncovered items",
              "inspect",
            ),
          );
        for (const name of await this.storage.list("steps")) {
          const step = await this.storage.read(`steps/${name}`, stepSchema);
          if (
            step?.verificationId === record.verificationId &&
            candidateIds.has(step.environmentId) &&
            (step.state !== "ended" || step.coverage !== "complete")
          )
            throw new VerificationError(
              problem(
                "coverageIncomplete",
                "All candidate observation steps must end with complete coverage",
              ),
            );
        }
        if (
          requiredChecks.some(
            (check) =>
              !check.evidenceIds.length ||
              check.evidenceIds.some(
                (id) =>
                  !evidence.some(
                    (item) =>
                      item.evidenceId === id &&
                      item.environmentId === check.environmentId,
                  ),
              ),
          )
        )
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "Check evidence is missing or belongs to another environment",
            ),
          );
        if (
          record.kind === "bug" &&
          (!checks.some(
            (check) =>
              baselineIds.has(check.environmentId) && check.status === "failed",
          ) ||
            !requiredChecks.some((candidate) =>
              checks.some(
                (baseline) =>
                  baselineIds.has(baseline.environmentId) &&
                  baseline.status === "failed" &&
                  (JSON.stringify(baseline.predicate) ===
                    JSON.stringify(candidate.predicate) ||
                    (baseline.comparisonKey &&
                      baseline.comparisonKey === candidate.comparisonKey &&
                      baseline.predicate.kind === candidate.predicate.kind &&
                      baseline.predicate.operator ===
                        candidate.predicate.operator &&
                      JSON.stringify(baseline.predicate.expected) ===
                        JSON.stringify(candidate.predicate.expected) &&
                      baseline.temporal.mode === candidate.temporal.mode)),
              ),
            ))
        )
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "A bug pass requires a matching baseline failure and candidate check",
            ),
          );
        if (
          record.requireScreenshots &&
          (!evidence.some(
            (item) =>
              item.kind === "screenshot" &&
              item.environmentId &&
              candidateIds.has(item.environmentId),
          ) ||
            (record.kind === "bug" &&
              !evidence.some(
                (item) =>
                  item.kind === "screenshot" &&
                  item.environmentId &&
                  baselineIds.has(item.environmentId),
              )))
        )
          throw new VerificationError(
            problem("coverageIncomplete", "Required screenshots are missing"),
          );
      }
      const environments: z.infer<typeof environmentSchema>[] = [];
      const datasets = [],
        dataSnapshots: Omit<z.infer<typeof dataSnapshotSchema>, "content">[] =
          [],
        steps = [],
        jobs: Json[] = [];
      const environmentIds = new Set(
        record.environments.map((item) => item.environmentId),
      );
      const refs = new Set([record.verificationId, ...environmentIds]);
      for (const name of await this.storage.list("datasets")) {
        const dataset = await this.storage.read(
          `datasets/${name}`,
          datasetSchema,
        );
        if (dataset && environmentIds.has(dataset.environmentId)) {
          datasets.push(dataset);
          refs.add(dataset.datasetId);
          if (
            dataset.dataSnapshotId &&
            !dataSnapshots.some(
              (item) => item.dataSnapshotId === dataset.dataSnapshotId,
            )
          ) {
            const snapshot = await this.storage.read(
              `data-snapshots/${dataset.dataSnapshotId}.json`,
              dataSnapshotSchema,
            );
            if (snapshot) {
              const { content: _, ...metadata } = snapshot;
              dataSnapshots.push(metadata);
            }
          }
        }
      }
      for (const name of await this.storage.list("steps")) {
        const step = await this.storage.read(`steps/${name}`, stepSchema);
        if (step?.verificationId === record.verificationId) {
          const { events, ...metadata } = step;
          steps.push({ ...metadata, eventCount: events.length });
          refs.add(step.stepId);
        }
      }
      for (const check of checks) refs.add(check.checkId);
      for (const name of await this.storage.list("jobs")) {
        const job = await this.storage.read(
          `jobs/${name}`,
          (await import("../contracts/index.js")).jobSchema,
        );
        if (job?.resourceRefs.some((ref) => refs.has(ref))) {
          const { output: _, ...metadata } = job;
          jobs.push(jsonSchema.parse(metadata));
        }
      }
      for (const entry of record.environments) {
        const environment = await this.storage.read(
          `environments/${entry.environmentId}.json`,
          environmentSchema,
        );
        if (environment) environments.push(environment);
      }
      const report = reportSchema.parse({
        schemaVersion: 1,
        reportRevision: randomUUID(),
        ...input,
        createdAt: new Date().toISOString(),
        evidence,
        checks,
        cleanup: environments.map((environment) => ({
          environmentId: environment.environmentId,
          state: environment.state,
          problem: environment.problem ?? null,
        })),
        environments,
        datasets,
        dataSnapshots,
        steps,
        jobs,
        plan: record.plan,
      });
      await this.storage.write(`reports/${report.reportRevision}.json`, report);
      record.revisions.push(report.reportRevision);
      record.state = "finalized";
      await this.storage.write(
        `verifications/${record.verificationId}.json`,
        record,
      );
      return report;
    });
  }
  async export(
    verificationId: string,
    revision: string,
    format: "json" | "markdown" | "html" | "bundle",
  ): Promise<{ path: string; reportRevision: string; sha256: string }> {
    const record = await this.records.read(verificationId);
    if (!record.revisions.includes(revision))
      throw new VerificationError(
        problem(
          "notFound",
          "The report revision is not part of this verification",
        ),
      );
    const report = await this.storage.read(
      `reports/${resourceIdSchema.parse(revision)}.json`,
      reportSchema,
    );
    if (!report)
      throw new VerificationError(
        problem("notFound", "Report revision is missing"),
      );
    if (format === "bundle") {
      const directory = join(
        this.storage.root,
        "reports",
        `${revision}-${randomUUID()}.bundle`,
      );
      await mkdir(directory, { mode: 0o700 });
      const files: { path: string; sha256: string }[] = [];
      for (const item of report.evidence) {
        const { bytes } = await this.read(item.evidenceId);
        const name = `artifacts/${basename(item.path)}`;
        await mkdir(join(directory, "artifacts"), {
          recursive: true,
          mode: 0o700,
        });
        await writeFile(join(directory, name), bytes, {
          mode: 0o600,
          flag: "wx",
        });
        files.push({ path: name, sha256: item.sha256 });
      }
      for (const metadata of report.dataSnapshots) {
        const snapshot = await this.storage.read(
          `data-snapshots/${metadata.dataSnapshotId}.json`,
          dataSnapshotSchema,
        );
        if (
          !snapshot ||
          sha256(
            Buffer.from(
              (await import("../host/storage.js")).canonicalJson(
                snapshot.content,
              ),
            ),
          ) !== metadata.sha256
        )
          throw new VerificationError(
            problem(
              "identityMismatch",
              "The report's immutable data snapshot is missing or changed",
            ),
          );
        for (const attachment of snapshot.attachments) {
          const expected = `data-snapshots/${snapshot.dataSnapshotId}.files/${sha256(Buffer.from(attachment.originalId))}`;
          if (attachment.relativePath !== expected)
            throw new VerificationError(
              problem("identityMismatch", "Snapshot attachment path changed"),
            );
          const bytes = await readFile(join(this.storage.root, expected));
          if (
            bytes.length !== attachment.bytes ||
            sha256(bytes) !== attachment.sha256
          )
            throw new VerificationError(
              problem(
                "identityMismatch",
                "Snapshot attachment integrity failed",
              ),
            );
          await mkdir(dirname(join(directory, expected)), { recursive: true });
          await writeFile(join(directory, expected), bytes, { mode: 0o600 });
          files.push({ path: expected, sha256: attachment.sha256 });
        }
        const path = `data-snapshots/${snapshot.dataSnapshotId}.json`,
          bytes = Buffer.from(JSON.stringify(snapshot));
        await mkdir(dirname(join(directory, path)), { recursive: true });
        await writeFile(join(directory, path), bytes, { mode: 0o600 });
        files.push({ path, sha256: sha256(bytes) });
      }
      const html = await this.export(verificationId, revision, "html"),
        bytes = await readFile(html.path);
      await writeFile(join(directory, "report.html"), bytes, { mode: 0o600 });
      files.push({ path: "report.html", sha256: sha256(bytes) });
      const metadata = Buffer.from(JSON.stringify(report, null, 2));
      await writeFile(join(directory, "report.json"), metadata, {
        mode: 0o600,
      });
      files.push({ path: "report.json", sha256: sha256(metadata) });
      const manifest = Buffer.from(
          JSON.stringify(
            {
              schemaVersion: 1,
              verificationId,
              reportRevision: revision,
              files,
            },
            null,
            2,
          ),
        ),
        path = join(directory, "manifest.json");
      await writeFile(path, manifest, { mode: 0o600 });
      return { path, reportRevision: revision, sha256: sha256(manifest) };
    }
    const images: { evidence: Evidence; bytes: Buffer }[] = [];
    for (const item of report.evidence) {
      const data = await this.read(item.evidenceId);
      if (item.kind === "screenshot") images.push(data);
    }
    let body = JSON.stringify(report, null, 2);
    if (format === "markdown")
      body =
        `# ${record.title}\n\n${report.conclusion}\n\n${report.explanation}\n\n` +
        report.checks
          .map(
            (check) =>
              `- ${check.predicate.kind}: ${check.status}; expected=${JSON.stringify(check.predicate.expected)}, actual=${JSON.stringify(check.actual ?? null)}`,
          )
          .join("\n") +
        "\n\n" +
        images
          .map(
            ({ evidence: item }) =>
              `![${item.source} ${item.environmentId ?? ""}](../${item.path})`,
          )
          .join("\n\n") +
        "\n\n" +
        report.uncovered.map((item) => `- Uncovered: ${item}`).join("\n");
    if (format === "html")
      body = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'"><title>${escape(record.title)}</title><style>body{max-width:960px;margin:32px auto;padding:16px;font:16px system-ui}img{max-width:100%}pre{white-space:pre-wrap;overflow-wrap:anywhere}</style><h1>${escape(record.title)}</h1><p>${escape(report.conclusion)}</p><pre>${escape(report.explanation)}</pre><h2>Checks</h2><pre>${escape(JSON.stringify(report.checks, null, 2))}</pre>${images.map(({ evidence: item, bytes }) => `<figure><img alt="${escape(item.source)}" src="data:${item.mediaType};base64,${bytes.toString("base64")}"><figcaption>${escape(item.environmentId ?? "")} / ${escape(item.stepId ?? "")}</figcaption></figure>`).join("")}<h2>Uncovered</h2><pre>${escape(report.uncovered.join("\n"))}</pre><h2>Sources and cleanup</h2><pre>${escape(JSON.stringify({ plan: report.plan, environments: report.environments, datasets: report.datasets, dataSnapshots: report.dataSnapshots, steps: report.steps, evidence: report.evidence, cleanup: report.cleanup }, null, 2))}</pre></html>`;
    const path = join(
      this.storage.root,
      "reports",
      `${revision}.${format === "markdown" ? "md" : format === "html" ? "html" : "json"}`,
    );
    if (format === "json") {
      const path = join(
        this.storage.root,
        "reports",
        `${revision}.export.json`,
      );
      await writeFile(path, body, { mode: 0o600 });
      return {
        path,
        reportRevision: revision,
        sha256: sha256(Buffer.from(body)),
      };
    }
    await writeFile(path, body, { mode: 0o600 });
    return {
      path,
      reportRevision: revision,
      sha256: sha256(Buffer.from(body)),
    };
  }
  pin(verificationId: string, pinned: boolean) {
    return this.serialize(async () => {
      const record = await this.records.read(verificationId);
      record.pinned = pinned;
      await this.storage.write(`verifications/${verificationId}.json`, record);
      return { verificationId, pinned };
    });
  }
  prune() {
    return this.serialize(async () => {
      const eligible = [];
      for (const name of await this.storage.list("verifications")) {
        const record = await this.storage.read(
          `verifications/${name}`,
          verificationSchema,
        );
        if (!record || record.state !== "finalized" || record.pinned) continue;
        let stopped = true;
        for (const entry of record.environments) {
          const environment = await this.storage.read(
            `environments/${entry.environmentId}.json`,
            environmentSchema,
          );
          if (!environment || environment.state !== "stopped") stopped = false;
        }
        if (stopped) eligible.push(record);
      }
      eligible.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
      const removed: string[] = [];
      for (const [index, record] of eligible.entries()) {
        if (
          index < 20 &&
          Date.parse(record.createdAt) >= Date.now() - 7 * 86400000
        )
          continue;
        const snapshots = new Set<string>();
        for (const name of await this.storage.list("steps")) {
          const step = await this.storage.read(`steps/${name}`, stepSchema);
          if (step?.verificationId === record.verificationId) {
            snapshots.add(step.startSnapshotId);
            if (step.endSnapshotId) snapshots.add(step.endSnapshotId);
          }
        }
        for (const name of await this.storage.list("evidence")) {
          const item = await this.storage.read(
            `evidence/${name}`,
            evidenceSchema,
          );
          if (
            item?.verificationId === record.verificationId &&
            item.kind === "snapshot"
          ) {
            const snapshot = stateSnapshotSchema.safeParse(
              JSON.parse(
                (await this.read(item.evidenceId)).bytes.toString("utf8"),
              ),
            );
            if (snapshot.success) snapshots.add(snapshot.data.stateSnapshotId);
          }
        }
        for (const category of ["evidence", "checks", "steps"] as const)
          for (const name of await this.storage.list(category)) {
            const schema = z.union([evidenceSchema, checkSchema, stepSchema]);
            const value = await this.storage.read(
              `${category}/${name}`,
              schema,
            );
            if (value?.verificationId === record.verificationId)
              await rm(join(this.storage.root, category, name));
          }
        for (const id of snapshots)
          await rm(
            join(
              this.storage.root,
              "state-snapshots",
              `${resourceIdSchema.parse(id)}.json`,
            ),
            { force: true },
          );
        await rm(join(this.storage.root, "artifacts", record.verificationId), {
          recursive: true,
          force: true,
        });
        for (const revision of record.revisions)
          for (const name of await this.storage.list("reports"))
            if (
              name === `${revision}.json` ||
              name.startsWith(`${revision}.`) ||
              name.startsWith(`${revision}-`)
            )
              await rm(join(this.storage.root, "reports", name), {
                recursive: true,
                force: true,
              });
        record.state = "pruned";
        record.revisions = [];
        await this.storage.write(
          `verifications/${record.verificationId}.json`,
          record,
        );
        removed.push(record.verificationId);
      }
      return {
        removed,
        policy: { days: 7, maximumUnpinnedRuns: 20 },
        preserves: [
          "active environments",
          "pinned reports",
          "data snapshots",
          "worktrees",
        ],
      };
    });
  }
  private serialize<T>(operation: () => Promise<T>): Promise<T> {
    const next = this.writes.catch(() => undefined).then(operation);
    this.writes = next;
    return next;
  }
}
function isImage(bytes: Uint8Array, mediaType: string): boolean {
  const data = Buffer.from(bytes);
  if (mediaType === "image/png")
    return (
      data.length >= 45 &&
      data
        .subarray(0, 8)
        .equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])) &&
      data.toString("ascii", 12, 16) === "IHDR" &&
      data.readUInt32BE(16) > 0 &&
      data.readUInt32BE(20) > 0 &&
      data.toString("ascii", data.length - 8, data.length - 4) === "IEND"
    );
  if (
    mediaType === "image/jpeg" &&
    data.length > 12 &&
    data[0] === 255 &&
    data[1] === 216 &&
    data[data.length - 2] === 255 &&
    data[data.length - 1] === 217
  ) {
    for (let offset = 2; offset + 9 < data.length; ) {
      if (data[offset] !== 255) return false;
      const marker = data[offset + 1],
        length = data.readUInt16BE(offset + 2);
      if (length < 2 || offset + 2 + length > data.length) return false;
      if (marker === 192 || marker === 194)
        return (
          data.readUInt16BE(offset + 5) > 0 && data.readUInt16BE(offset + 7) > 0
        );
      offset += length + 2;
    }
  }
  return false;
}
