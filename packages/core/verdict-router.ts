/**
 * Reviewer Verdict Router — Driver-plane state machine v1
 *
 * Parses structured reviewer-verdict envelopes from issue comments and
 * produces routing decisions (create fix task, escalate, merge gate, etc.)
 * without requiring agent @mention links.
 *
 * Spec: docs/reviewer-verdict-routing-v1.md
 * Schema: docs/reviewer-verdict-v1.md (DRV-79)
 */

// ── Types ────────────────────────────────────────────────────────────────────

export type VerdictValue = "APPROVED" | "WARNING" | "REQUEST_CHANGES" | "BLOCKER";

export type FindingSeverity = "info" | "low" | "medium" | "high" | "critical";

export interface FindingLocation {
  file: string | null;
  line: number | null;
  range: { start: number; end: number } | null;
  commit_sha: string;
}

export interface Finding {
  id: string;
  severity: FindingSeverity;
  check: string;
  rationale: string;
  required_action: string;
  location: FindingLocation;
  tags: string[];
}

export interface Verification {
  name: string;
  status: "passed" | "failed" | "skipped";
  evidence: string;
}

export interface VerdictEnvelope {
  schema_version: string;
  verdict: VerdictValue;
  verdict_id: string;
  pr: {
    url: string;
    head_sha: string;
    base_sha: string;
  };
  reviewer: {
    agent_id: string;
    agent_name: string;
    model?: string;
  };
  issued_at: string;
  findings: Finding[];
  verifications: Verification[];
  summary: string;
  next_review_required: boolean;
}

export type ParseErrorKind =
  | "malformed-envelope"
  | "unsupported-schema-version"
  | "verdict-severity-mismatch"
  | "incomplete-envelope"
  | "cap-exceeded";

export interface ParseSuccess {
  ok: true;
  envelope: VerdictEnvelope;
  warnings: ParseErrorKind[];
}

export interface ParseFailure {
  ok: false;
  error: ParseErrorKind;
  rawContent: string;
}

export type ParseResult = ParseSuccess | ParseFailure;

export type RoutingAction =
  | "MERGE_GATE"       // APPROVED or WARNING — proceed to merge
  | "CREATE_TASK"      // Create new fix task
  | "REUSE_TASK"       // Idempotent — existing task already covers this verdict
  | "ESCALATE"         // Human required
  | "AUDIT_ONLY";      // Log finding, no task, no escalation (malformed etc.)

export interface ExistingFixTask {
  id: string;
  identifier: string;
  idempotencyKey: string;
}

export interface RoutingContext {
  /** Current review iteration count (1-based, incremented after each FIX_IN_PROGRESS). */
  reviewCycle: number;
  /** Max fix cycles before escalating. Default 3. */
  maxCycles?: number;
  /** Existing child fix tasks for this issue (used for idempotency lookup). */
  existingFixTasks: ExistingFixTask[];
}

export interface RoutingDecision {
  action: RoutingAction;
  /** Set when action=CREATE_TASK or REUSE_TASK. */
  fixTaskId?: string;
  /** Idempotency key used for lookup/creation. */
  idempotencyKey?: string;
  /** Human-readable reason, used in audit comment. */
  reason: string;
  /** Escalation reason when action=ESCALATE. */
  escalationReason?: string;
}

// ── Constants ────────────────────────────────────────────────────────────────

const VERDICT_MARKER = "<!-- multica:reviewer-verdict v1 -->";
const FIX_TASK_KEY_PATTERN = /<!--\s*multica:fix-task-key\s+(\S+)\s*-->/;
const FINDING_CAP = 20;
const SUPPORTED_SCHEMA_VERSIONS = new Set(["1"]);
const SEVERITY_RANK: Record<FindingSeverity, number> = {
  info: 0,
  low: 1,
  medium: 2,
  high: 3,
  critical: 4,
};

/** Checks that escalate to human instead of creating a fix task. */
const ESCALATE_CHECKS = new Set([
  "scope-change",
  "product-ambiguity",
  "destructive-operation",
  "auth-failure",
  "permission-denied",
]);

// ── Parsing ───────────────────────────────────────────────────────────────────

/**
 * Derive the correct verdict from findings — overrides claimed verdict when
 * inconsistent (prevents reviewer from saying APPROVED while listing critical).
 */
function deriveVerdictFromFindings(findings: Finding[]): VerdictValue {
  if (findings.length === 0) return "APPROVED";
  const max = findings.reduce((best, f) => {
    return SEVERITY_RANK[f.severity] > SEVERITY_RANK[best] ? f.severity : best;
  }, "info" as FindingSeverity);
  if (max === "critical") return "BLOCKER";
  if (max === "high") return "REQUEST_CHANGES";
  if (max === "medium" || max === "low") return "WARNING";
  return "APPROVED";
}

/**
 * Extract and validate the fenced verdict envelope from a comment string.
 * Returns ParseSuccess with any warnings, or ParseFailure on hard errors.
 */
export function parseVerdictFromComment(content: string): ParseResult {
  const markerIdx = content.indexOf(VERDICT_MARKER);
  if (markerIdx === -1) {
    // No structured envelope — driver falls back to prose heuristics upstream
    return { ok: false, error: "malformed-envelope", rawContent: content };
  }

  // Extract JSON from fenced block after marker
  const afterMarker = content.slice(markerIdx + VERDICT_MARKER.length);
  const fenceMatch = afterMarker.match(/```(?:json)?\s*\n([\s\S]*?)```/);
  if (!fenceMatch) {
    return { ok: false, error: "malformed-envelope", rawContent: content };
  }

  let raw: unknown;
  try {
    raw = JSON.parse(fenceMatch[1] ?? "");
  } catch {
    return { ok: false, error: "malformed-envelope", rawContent: content };
  }

  if (typeof raw !== "object" || raw === null) {
    return { ok: false, error: "malformed-envelope", rawContent: content };
  }

  const obj = raw as Record<string, unknown>;
  const warnings: ParseErrorKind[] = [];

  // schema_version check
  const schemaVersion = String(obj["schema_version"] ?? "");
  if (!SUPPORTED_SCHEMA_VERSIONS.has(schemaVersion)) {
    return { ok: false, error: "unsupported-schema-version", rawContent: content };
  }

  // Required fields
  const requiredFields = ["verdict", "verdict_id", "pr", "reviewer", "issued_at", "findings"] as const;
  for (const field of requiredFields) {
    if (!(field in obj) || obj[field] === null || obj[field] === undefined) {
      return { ok: false, error: "incomplete-envelope", rawContent: content };
    }
  }

  const pr = obj["pr"] as Record<string, unknown>;
  if (!pr["head_sha"] || !pr["url"] || !pr["base_sha"]) {
    return { ok: false, error: "incomplete-envelope", rawContent: content };
  }

  const reviewer = obj["reviewer"] as Record<string, unknown>;
  if (!reviewer["agent_id"] || !reviewer["agent_name"]) {
    return { ok: false, error: "incomplete-envelope", rawContent: content };
  }

  // Findings — cap at FINDING_CAP
  let findings = (obj["findings"] as Finding[]) ?? [];
  if (findings.length > FINDING_CAP) {
    const omittedCount = findings.length - (FINDING_CAP - 1);
    findings = findings.slice(0, FINDING_CAP - 1);
    findings.push({
      id: "truncation-marker",
      severity: "info",
      check: "cap-exceeded",
      rationale: `${omittedCount} additional findings omitted`,
      required_action: "Review full finding list in the reviewer's session output.",
      location: { file: null, line: null, range: null, commit_sha: String(pr["head_sha"]) },
      tags: ["meta"],
    });
    warnings.push("cap-exceeded");
  }

  // Verdict/severity consistency
  const claimedVerdict = String(obj["verdict"]) as VerdictValue;
  const derivedVerdict = deriveVerdictFromFindings(findings);
  const resolvedVerdict =
    claimedVerdict === derivedVerdict ? claimedVerdict : derivedVerdict;
  if (claimedVerdict !== derivedVerdict) {
    warnings.push("verdict-severity-mismatch");
  }

  const envelope: VerdictEnvelope = {
    schema_version: schemaVersion,
    verdict: resolvedVerdict,
    verdict_id: String(obj["verdict_id"]),
    pr: {
      url: String(pr["url"]),
      head_sha: String(pr["head_sha"]),
      base_sha: String(pr["base_sha"]),
    },
    reviewer: {
      agent_id: String(reviewer["agent_id"]),
      agent_name: String(reviewer["agent_name"]),
      model: reviewer["model"] ? String(reviewer["model"]) : undefined,
    },
    issued_at: String(obj["issued_at"]),
    findings,
    verifications: (obj["verifications"] as Verification[]) ?? [],
    summary: String(obj["summary"] ?? ""),
    next_review_required: Boolean(obj["next_review_required"]),
  };

  return { ok: true, envelope, warnings };
}

// ── Idempotency ───────────────────────────────────────────────────────────────

/** Returns the idempotency key for a verdict: `{head_sha}:{verdict_id}` */
export function buildIdempotencyKey(headSha: string, verdictId: string): string {
  return `${headSha}:${verdictId}`;
}

/** Extracts the fix-task idempotency key tag from a task description. */
export function extractFixTaskKey(description: string): string | null {
  const m = description.match(FIX_TASK_KEY_PATTERN);
  return m?.[1] ?? null;
}

/** Builds the idempotency tag to embed in a fix task description. */
export function buildFixTaskKeyTag(key: string): string {
  return `<!-- multica:fix-task-key ${key} -->`;
}

// ── Escalation classification ─────────────────────────────────────────────────

function shouldEscalateBlocker(findings: Finding[]): { escalate: boolean; reason: string } {
  for (const f of findings) {
    if (ESCALATE_CHECKS.has(f.check)) {
      return {
        escalate: true,
        reason: `BLOCKER finding \`${f.check}\` requires human decision: ${f.rationale.slice(0, 200)}`,
      };
    }
    if (f.required_action.toLowerCase().includes("human")) {
      return {
        escalate: true,
        reason: `BLOCKER finding \`${f.check}\` requires human action: ${f.required_action.slice(0, 200)}`,
      };
    }
  }
  return { escalate: false, reason: "" };
}

// ── Routing ───────────────────────────────────────────────────────────────────

/**
 * Given a parsed VerdictEnvelope and the current routing context, returns a
 * RoutingDecision describing what the driver should do next.
 *
 * Does NOT perform any side effects — caller is responsible for creating tasks,
 * updating issue status, and posting audit comments.
 */
export function routeVerdict(
  envelope: VerdictEnvelope,
  context: RoutingContext
): RoutingDecision {
  const maxCycles = context.maxCycles ?? 3;
  const { verdict, pr, verdict_id } = envelope;
  const idempotencyKey = buildIdempotencyKey(pr.head_sha, verdict_id);

  switch (verdict) {
    case "APPROVED":
    case "WARNING": {
      return {
        action: "MERGE_GATE",
        idempotencyKey,
        reason:
          verdict === "APPROVED"
            ? "All checks passed. Routing to merge gate."
            : `${envelope.findings.filter((f) => ["low", "medium"].includes(f.severity)).length} low/medium finding(s). Non-blocking — routing to merge gate.`,
      };
    }

    case "REQUEST_CHANGES":
    case "BLOCKER": {
      // Check cycle limit first
      if (context.reviewCycle >= maxCycles) {
        return {
          action: "ESCALATE",
          idempotencyKey,
          reason: `${context.reviewCycle} fix cycle(s) completed without resolution. Escalating for human review.`,
          escalationReason: `Exceeded max fix cycles (${maxCycles}).`,
        };
      }

      // For BLOCKERs: classify findings to decide escalate vs fix task
      if (verdict === "BLOCKER") {
        const { escalate, reason } = shouldEscalateBlocker(envelope.findings);
        if (escalate) {
          return {
            action: "ESCALATE",
            idempotencyKey,
            reason,
            escalationReason: reason,
          };
        }
      }

      // Idempotency: check if fix task for this key already exists
      const existing = context.existingFixTasks.find(
        (t) => t.idempotencyKey === idempotencyKey
      );
      if (existing) {
        return {
          action: "REUSE_TASK",
          fixTaskId: existing.id,
          idempotencyKey,
          reason: `Fix task ${existing.identifier} already covers verdict ${verdict_id.slice(0, 8)}.`,
        };
      }

      return {
        action: "CREATE_TASK",
        idempotencyKey,
        reason:
          verdict === "BLOCKER"
            ? `BLOCKER: ${envelope.findings.filter((f) => f.severity === "critical").length} critical finding(s) must be fixed before merge.`
            : `REQUEST_CHANGES: ${envelope.findings.filter((f) => f.severity === "high").length} high finding(s) require fixes.`,
      };
    }
  }
}

/**
 * Build a routing decision for a failed parse result.
 * Malformed envelopes produce AUDIT_ONLY or ESCALATE (incomplete-envelope = fail closed).
 */
export function routeMalformedVerdict(failure: ParseFailure): RoutingDecision {
  if (failure.error === "incomplete-envelope") {
    return {
      action: "ESCALATE",
      reason: "Reviewer envelope missing required fields — failing closed. Human review needed.",
      escalationReason: `Malformed reviewer envelope: ${failure.error}`,
    };
  }
  return {
    action: "AUDIT_ONLY",
    reason: `Reviewer envelope could not be parsed (${failure.error}). No task created.`,
  };
}

// ── Fix task content helpers ──────────────────────────────────────────────────

export interface FixTaskDraft {
  title: string;
  description: string;
}

/**
 * Build the title and description for a new fix task.
 * Caller is responsible for setting assignee, parent, project, etc.
 */
export function buildFixTaskDraft(
  envelope: VerdictEnvelope,
  parentIdentifier: string,
  idempotencyKey: string
): FixTaskDraft {
  const { verdict, verdict_id, pr, findings, verifications } = envelope;
  const shortVerdictId = verdict_id.slice(0, 8);
  const shortSha = pr.head_sha.slice(0, 7);

  const scope = parentIdentifier.toLowerCase().replace(/[^a-z0-9-]/g, "-");
  const title = `fix(${scope}): address ${verdict} from review ${shortVerdictId}`;

  const actionableFindings = findings
    .filter((f) => SEVERITY_RANK[f.severity] >= SEVERITY_RANK["high"])
    .slice(0, 10);

  const findingLines = actionableFindings
    .map((f) => {
      const loc = f.location.file ? `\n    File: ${f.location.file}${f.location.line ? `:${f.location.line}` : ""}` : "";
      return `- [${f.severity}] \`${f.check}\` — ${f.rationale}\n    Fix: ${f.required_action}${loc}`;
    })
    .join("\n\n");

  const lowerFindingCount = findings.filter(
    (f) => SEVERITY_RANK[f.severity] < SEVERITY_RANK["high"]
  ).length;

  const failedVerifications = verifications.filter((v) => v.status === "failed");
  const verificationLines = failedVerifications.length > 0
    ? "\n\n## Verifications that must pass before re-review\n\n" +
      failedVerifications.map((v) => `- [ ] ${v.name}`).join("\n")
    : "";

  const truncationNote =
    lowerFindingCount > 0
      ? `\n\n… plus ${lowerFindingCount} lower-severity finding(s). See verdict comment for full list.`
      : "";

  const description = [
    `Parent review: ${parentIdentifier} · PR: ${pr.url} @ \`${shortSha}\``,
    "",
    buildFixTaskKeyTag(idempotencyKey),
    "",
    "## Findings to address",
    "",
    findingLines || "_No high/critical findings listed — check full verdict comment._",
    truncationNote,
    verificationLines,
    "",
    "Re-push when fixed. No @mention needed — assignment wakes this task automatically.",
  ]
    .join("\n")
    .trim();

  return { title, description };
}

// ── Audit comment helpers ─────────────────────────────────────────────────────

export interface AuditCommentContext {
  iterationNumber: number;
  envelope: VerdictEnvelope;
  decision: RoutingDecision;
  fixTaskIdentifier?: string;
  fixTaskId?: string;
}

/**
 * Build the single audit comment the driver posts per review iteration.
 * No agent @mention links — issue-mention links only.
 */
export function buildAuditComment(ctx: AuditCommentContext): string {
  const { iterationNumber, envelope, decision, fixTaskIdentifier, fixTaskId } = ctx;
  const { verdict, verdict_id } = envelope;
  const shortId = verdict_id.slice(0, 8);

  const header = `**Review iteration ${iterationNumber}** · verdict \`${verdict}\` · ${shortId}`;

  let body: string;
  switch (decision.action) {
    case "MERGE_GATE":
      if (verdict === "APPROVED") {
        body = "All checks passed. Routing to merge gate.";
      } else {
        const count = envelope.findings.filter((f) =>
          ["low", "medium"].includes(f.severity)
        ).length;
        const lines = envelope.findings
          .filter((f) => ["low", "medium"].includes(f.severity))
          .slice(0, 5)
          .map((f) => `- [${f.severity}] \`${f.check}\`: ${f.rationale.slice(0, 120)}`)
          .join("\n");
        body = `${count} low/medium finding(s). Non-blocking — proceeding to merge gate.\n\n${lines}`;
      }
      break;

    case "CREATE_TASK":
    case "REUSE_TASK": {
      const taskRef =
        fixTaskIdentifier && fixTaskId
          ? `[${fixTaskIdentifier}](mention://issue/${fixTaskId})`
          : "_fix task_";
      const count =
        verdict === "BLOCKER"
          ? envelope.findings.filter((f) => f.severity === "critical").length
          : envelope.findings.filter((f) => f.severity === "high").length;
      const severity = verdict === "BLOCKER" ? "critical" : "high";
      body =
        decision.action === "REUSE_TASK"
          ? `${count} ${severity} finding(s) require fixes. Fix task ${taskRef} already exists — no duplicate created.`
          : `${count} ${severity} finding(s) require fixes. Fix task: ${taskRef}.\nImplementer will push when done; reviewer will be re-requested automatically.`;
      break;
    }

    case "ESCALATE":
      body = `Escalated: ${decision.escalationReason ?? decision.reason}\n\nHuman decision required before this can proceed.`;
      break;

    case "AUDIT_ONLY":
      body = `⚠ ${decision.reason}`;
      break;
  }

  return `${header}\n\n${body}`;
}
