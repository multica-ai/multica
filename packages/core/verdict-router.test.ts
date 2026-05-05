import { describe, it, expect } from "vitest";
import {
  parseVerdictFromComment,
  routeVerdict,
  routeMalformedVerdict,
  buildIdempotencyKey,
  extractFixTaskKey,
  buildFixTaskKeyTag,
  buildFixTaskDraft,
  buildAuditComment,
  type VerdictEnvelope,
  type RoutingContext,
  type ExistingFixTask,
} from "./verdict-router";

// ── Fixtures ──────────────────────────────────────────────────────────────────

function makeEnvelope(overrides: Partial<VerdictEnvelope> = {}): VerdictEnvelope {
  return {
    schema_version: "1",
    verdict: "APPROVED",
    verdict_id: "aaaaaaaa-0000-0000-0000-000000000001",
    pr: { url: "https://github.com/multica-ai/multica/pull/1", head_sha: "abc1234", base_sha: "f0e9d8c" },
    reviewer: { agent_id: "rev-001", agent_name: "codex-reviewer" },
    issued_at: "2026-05-05T10:00:00Z",
    findings: [],
    verifications: [{ name: "pnpm test", status: "passed", evidence: "all green" }],
    summary: "Looks good.",
    next_review_required: false,
    ...overrides,
  };
}

function wrapInComment(envelope: VerdictEnvelope): string {
  return `Some prose before.\n\n<!-- multica:reviewer-verdict v1 -->\n\`\`\`json\n${JSON.stringify(envelope, null, 2)}\n\`\`\`\n\nSome prose after.`;
}

function makeContext(overrides: Partial<RoutingContext> = {}): RoutingContext {
  return { reviewCycle: 1, existingFixTasks: [], ...overrides };
}

function makeFixTask(idempotencyKey: string): ExistingFixTask {
  return { id: "task-001", identifier: "DRV-99", idempotencyKey };
}

// ── Parse tests ───────────────────────────────────────────────────────────────

describe("parseVerdictFromComment", () => {
  it("parses a valid APPROVED envelope", () => {
    const env = makeEnvelope({ verdict: "APPROVED" });
    const result = parseVerdictFromComment(wrapInComment(env));
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.envelope.verdict).toBe("APPROVED");
    expect(result.envelope.verdict_id).toBe(env.verdict_id);
    expect(result.warnings).toHaveLength(0);
  });

  it("parses a valid REQUEST_CHANGES envelope with findings", () => {
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      findings: [
        {
          id: "sha256:abc",
          severity: "high",
          check: "missing-test",
          rationale: "Branch without coverage.",
          required_action: "Add test.",
          location: { file: "src/foo.ts", line: 10, range: null, commit_sha: "abc1234" },
          tags: ["tests"],
        },
      ],
    });
    const result = parseVerdictFromComment(wrapInComment(env));
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.envelope.verdict).toBe("REQUEST_CHANGES");
    expect(result.envelope.findings).toHaveLength(1);
  });

  it("parses a valid BLOCKER envelope", () => {
    const env = makeEnvelope({
      verdict: "BLOCKER",
      findings: [
        {
          id: "sha256:def",
          severity: "critical",
          check: "p0-cancellation-rethrow",
          rationale: "Silent deadlock risk.",
          required_action: "Re-throw CancellationException.",
          location: { file: "src/dispatcher.ts", line: 57, range: null, commit_sha: "abc1234" },
          tags: ["concurrency", "p0"],
        },
      ],
    });
    const result = parseVerdictFromComment(wrapInComment(env));
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.envelope.verdict).toBe("BLOCKER");
  });

  it("parses a valid WARNING envelope", () => {
    const env = makeEnvelope({
      verdict: "WARNING",
      findings: [
        {
          id: "sha256:ghi",
          severity: "low",
          check: "naming-convention",
          rationale: "Unclear name.",
          required_action: "Rename.",
          location: { file: null, line: null, range: null, commit_sha: "abc1234" },
          tags: ["style"],
        },
      ],
    });
    const result = parseVerdictFromComment(wrapInComment(env));
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.envelope.verdict).toBe("WARNING");
  });

  it("returns malformed-envelope for prose-only comment", () => {
    const result = parseVerdictFromComment("LGTM, looks great!");
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error).toBe("malformed-envelope");
  });

  it("returns malformed-envelope for garbled JSON", () => {
    const comment = `<!-- multica:reviewer-verdict v1 -->\n\`\`\`json\n{ not valid json }\n\`\`\``;
    const result = parseVerdictFromComment(comment);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error).toBe("malformed-envelope");
  });

  it("returns unsupported-schema-version for unknown version", () => {
    const env = makeEnvelope({ schema_version: "99" } as Partial<VerdictEnvelope>);
    const result = parseVerdictFromComment(wrapInComment(env));
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error).toBe("unsupported-schema-version");
  });

  it("corrects verdict/severity mismatch and emits warning", () => {
    // Reviewer claims APPROVED but findings include critical — should derive BLOCKER
    const env = makeEnvelope({
      verdict: "APPROVED",
      findings: [
        {
          id: "sha256:mismatch",
          severity: "critical",
          check: "sql-injection",
          rationale: "SQL injection.",
          required_action: "Use parameterized query.",
          location: { file: "db.ts", line: 5, range: null, commit_sha: "abc1234" },
          tags: ["security"],
        },
      ],
    });
    const result = parseVerdictFromComment(wrapInComment(env));
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.envelope.verdict).toBe("BLOCKER"); // overridden
    expect(result.warnings).toContain("verdict-severity-mismatch");
  });

  it("returns incomplete-envelope when required fields missing", () => {
    const env = makeEnvelope();
    const broken = { ...env } as Record<string, unknown>;
    delete broken["verdict_id"];
    const comment = `<!-- multica:reviewer-verdict v1 -->\n\`\`\`json\n${JSON.stringify(broken)}\n\`\`\``;
    const result = parseVerdictFromComment(comment);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error).toBe("incomplete-envelope");
  });

  it("trims findings to cap and adds cap-exceeded marker", () => {
    const findings = Array.from({ length: 25 }, (_, i) => ({
      id: `sha256:f${i}`,
      severity: "high" as const,
      check: `check-${i}`,
      rationale: `Rationale ${i}`,
      required_action: `Fix ${i}`,
      location: { file: `file${i}.ts`, line: i + 1, range: null, commit_sha: "abc1234" },
      tags: [],
    }));
    const env = makeEnvelope({ verdict: "REQUEST_CHANGES", findings });
    const result = parseVerdictFromComment(wrapInComment(env));
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    const success = result;
    expect(success.envelope.findings).toHaveLength(20);
    expect(success.envelope.findings[19]!.check).toBe("cap-exceeded");
    expect(success.warnings).toContain("cap-exceeded");
  });
});

// ── Idempotency key tests ─────────────────────────────────────────────────────

describe("buildIdempotencyKey", () => {
  it("combines head_sha and verdict_id", () => {
    expect(buildIdempotencyKey("abc1234", "uuid-1")).toBe("abc1234:uuid-1");
  });
});

describe("extractFixTaskKey", () => {
  it("extracts key from description tag", () => {
    const desc = `Some text\n${buildFixTaskKeyTag("abc1234:uuid-1")}\nMore text`;
    expect(extractFixTaskKey(desc)).toBe("abc1234:uuid-1");
  });

  it("returns null when no tag present", () => {
    expect(extractFixTaskKey("No tag here")).toBeNull();
  });
});

// ── Routing tests ─────────────────────────────────────────────────────────────

describe("routeVerdict", () => {
  it("routes APPROVED to MERGE_GATE", () => {
    const env = makeEnvelope({ verdict: "APPROVED" });
    const decision = routeVerdict(env, makeContext());
    expect(decision.action).toBe("MERGE_GATE");
  });

  it("routes WARNING to MERGE_GATE", () => {
    const env = makeEnvelope({
      verdict: "WARNING",
      findings: [
        {
          id: "sha256:w1",
          severity: "low",
          check: "style",
          rationale: "Minor.",
          required_action: "Rename.",
          location: { file: null, line: null, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    const decision = routeVerdict(env, makeContext());
    expect(decision.action).toBe("MERGE_GATE");
  });

  it("routes REQUEST_CHANGES to CREATE_TASK when no existing task", () => {
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      findings: [
        {
          id: "sha256:rc1",
          severity: "high",
          check: "missing-test",
          rationale: "Missing test.",
          required_action: "Add test.",
          location: { file: "foo.ts", line: 1, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    const decision = routeVerdict(env, makeContext());
    expect(decision.action).toBe("CREATE_TASK");
    expect(decision.idempotencyKey).toBe(
      buildIdempotencyKey(env.pr.head_sha, env.verdict_id)
    );
  });

  it("routes duplicate verdict (same idempotency key) to REUSE_TASK", () => {
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      findings: [
        {
          id: "sha256:rc1",
          severity: "high",
          check: "missing-test",
          rationale: "Missing test.",
          required_action: "Add test.",
          location: { file: "foo.ts", line: 1, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    const key = buildIdempotencyKey(env.pr.head_sha, env.verdict_id);
    const ctx = makeContext({ existingFixTasks: [makeFixTask(key)] });
    const decision = routeVerdict(env, ctx);
    expect(decision.action).toBe("REUSE_TASK");
    expect(decision.fixTaskId).toBe("task-001");
  });

  it("creates new task when head SHA moved (new commit, same verdict struct)", () => {
    // New head SHA = new idempotency key even if verdict_id is different
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      pr: { url: "https://...", head_sha: "newsha99", base_sha: "f0e9d8c" },
      verdict_id: "bbbbbbbb-0000-0000-0000-000000000002",
      findings: [
        {
          id: "sha256:rc2",
          severity: "high",
          check: "missing-test",
          rationale: "Missing test.",
          required_action: "Add test.",
          location: { file: "foo.ts", line: 1, range: null, commit_sha: "newsha99" },
          tags: [],
        },
      ],
    });
    // Old task has old key (different sha + verdict_id)
    const oldKey = buildIdempotencyKey("abc1234", "aaaaaaaa-0000-0000-0000-000000000001");
    const ctx = makeContext({ existingFixTasks: [makeFixTask(oldKey)] });
    const decision = routeVerdict(env, ctx);
    expect(decision.action).toBe("CREATE_TASK");
    expect(decision.idempotencyKey).toBe("newsha99:bbbbbbbb-0000-0000-0000-000000000002");
  });

  it("routes BLOCKER with code fix to CREATE_TASK", () => {
    const env = makeEnvelope({
      verdict: "BLOCKER",
      findings: [
        {
          id: "sha256:bl1",
          severity: "critical",
          check: "p0-cancellation-rethrow",
          rationale: "Silent deadlock.",
          required_action: "Re-throw CancellationException.",
          location: { file: "dispatcher.ts", line: 57, range: null, commit_sha: "abc1234" },
          tags: ["p0"],
        },
      ],
    });
    const decision = routeVerdict(env, makeContext());
    expect(decision.action).toBe("CREATE_TASK");
  });

  it("escalates BLOCKER with scope-change check", () => {
    const env = makeEnvelope({
      verdict: "BLOCKER",
      findings: [
        {
          id: "sha256:sc1",
          severity: "critical",
          check: "scope-change",
          rationale: "This PR changes product scope beyond the issue.",
          required_action: "Human must decide whether to proceed.",
          location: { file: null, line: null, range: null, commit_sha: "abc1234" },
          tags: ["scope"],
        },
      ],
    });
    const decision = routeVerdict(env, makeContext());
    expect(decision.action).toBe("ESCALATE");
  });

  it("escalates BLOCKER with destructive-operation check", () => {
    const env = makeEnvelope({
      verdict: "BLOCKER",
      findings: [
        {
          id: "sha256:do1",
          severity: "critical",
          check: "destructive-operation",
          rationale: "Drops production table.",
          required_action: "Human approval required.",
          location: { file: "migration.sql", line: 1, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    const decision = routeVerdict(env, makeContext());
    expect(decision.action).toBe("ESCALATE");
  });

  it("escalates when cycle limit reached on REQUEST_CHANGES", () => {
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      findings: [
        {
          id: "sha256:cl1",
          severity: "high",
          check: "missing-test",
          rationale: "Still missing.",
          required_action: "Add test.",
          location: { file: "foo.ts", line: 1, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    const ctx = makeContext({ reviewCycle: 3 });
    const decision = routeVerdict(env, ctx);
    expect(decision.action).toBe("ESCALATE");
    expect(decision.reason).toContain("fix cycle");
  });

  it("respects custom maxCycles", () => {
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      findings: [
        {
          id: "sha256:mc1",
          severity: "high",
          check: "missing-test",
          rationale: "Still.",
          required_action: "Add test.",
          location: { file: "foo.ts", line: 1, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    // maxCycles=5, cycle=3 → should still CREATE_TASK
    const ctx = makeContext({ reviewCycle: 3, maxCycles: 5 });
    const decision = routeVerdict(env, ctx);
    expect(decision.action).toBe("CREATE_TASK");

    // At cycle 5 → ESCALATE
    const ctx5 = makeContext({ reviewCycle: 5, maxCycles: 5 });
    const decision5 = routeVerdict(env, ctx5);
    expect(decision5.action).toBe("ESCALATE");
  });
});

describe("routeMalformedVerdict", () => {
  it("returns AUDIT_ONLY for malformed-envelope", () => {
    const result = routeMalformedVerdict({ ok: false, error: "malformed-envelope", rawContent: "" });
    expect(result.action).toBe("AUDIT_ONLY");
  });

  it("returns ESCALATE for incomplete-envelope (fail closed)", () => {
    const result = routeMalformedVerdict({ ok: false, error: "incomplete-envelope", rawContent: "" });
    expect(result.action).toBe("ESCALATE");
  });
});

// ── Fix task draft tests ──────────────────────────────────────────────────────

describe("buildFixTaskDraft", () => {
  it("title contains verdict type and short verdict_id", () => {
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      verdict_id: "aaaaaaaa-1234-0000-0000-000000000001",
      findings: [
        {
          id: "sha256:f1",
          severity: "high",
          check: "missing-test",
          rationale: "No test.",
          required_action: "Add test.",
          location: { file: "foo.ts", line: 1, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    const key = buildIdempotencyKey(env.pr.head_sha, env.verdict_id);
    const draft = buildFixTaskDraft(env, "DRV-80", key);
    expect(draft.title).toContain("REQUEST_CHANGES");
    expect(draft.title).toContain("aaaaaaaa");
  });

  it("description embeds idempotency key tag", () => {
    const env = makeEnvelope({ verdict: "REQUEST_CHANGES" });
    const key = buildIdempotencyKey(env.pr.head_sha, env.verdict_id);
    const draft = buildFixTaskDraft(env, "DRV-80", key);
    expect(draft.description).toContain(`<!-- multica:fix-task-key ${key} -->`);
  });

  it("description includes high/critical findings", () => {
    const env = makeEnvelope({
      verdict: "BLOCKER",
      findings: [
        {
          id: "sha256:bl1",
          severity: "critical",
          check: "sql-injection",
          rationale: "SQL injection risk.",
          required_action: "Parameterize query.",
          location: { file: "db.ts", line: 10, range: null, commit_sha: "abc1234" },
          tags: ["security"],
        },
      ],
    });
    const key = buildIdempotencyKey(env.pr.head_sha, env.verdict_id);
    const draft = buildFixTaskDraft(env, "DRV-80", key);
    expect(draft.description).toContain("sql-injection");
    expect(draft.description).toContain("Parameterize query");
  });

  it("description omits low findings but counts them", () => {
    const findings = [
      {
        id: "sha256:h1",
        severity: "high" as const,
        check: "high-check",
        rationale: "High issue.",
        required_action: "Fix high.",
        location: { file: "a.ts", line: 1, range: null, commit_sha: "abc1234" },
        tags: [],
      },
      {
        id: "sha256:l1",
        severity: "low" as const,
        check: "style",
        rationale: "Minor style.",
        required_action: "Rename.",
        location: { file: "b.ts", line: 2, range: null, commit_sha: "abc1234" },
        tags: [],
      },
    ];
    const env = makeEnvelope({ verdict: "REQUEST_CHANGES", findings });
    const key = buildIdempotencyKey(env.pr.head_sha, env.verdict_id);
    const draft = buildFixTaskDraft(env, "DRV-80", key);
    expect(draft.description).toContain("high-check");
    expect(draft.description).toContain("1 lower-severity finding");
    expect(draft.description).not.toContain("style\n");
  });

  it("includes failed verifications checklist", () => {
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      findings: [
        {
          id: "sha256:f1",
          severity: "high",
          check: "c",
          rationale: "r",
          required_action: "f",
          location: { file: null, line: null, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
      verifications: [
        { name: "pnpm test", status: "failed", evidence: "3 failures" },
        { name: "pnpm typecheck", status: "passed", evidence: "ok" },
      ],
    });
    const key = buildIdempotencyKey(env.pr.head_sha, env.verdict_id);
    const draft = buildFixTaskDraft(env, "DRV-80", key);
    expect(draft.description).toContain("pnpm test");
    expect(draft.description).not.toContain("pnpm typecheck");
  });
});

// ── Audit comment tests ───────────────────────────────────────────────────────

describe("buildAuditComment", () => {
  it("APPROVED: mentions iteration + verdict + routing to merge gate", () => {
    const env = makeEnvelope({ verdict: "APPROVED" });
    const decision = routeVerdict(env, makeContext());
    const comment = buildAuditComment({ iterationNumber: 1, envelope: env, decision });
    expect(comment).toContain("Review iteration 1");
    expect(comment).toContain("APPROVED");
    expect(comment).toContain("merge gate");
    expect(comment).not.toMatch(/mention:\/\/agent/);
  });

  it("REQUEST_CHANGES with task: contains fix task issue-mention link", () => {
    const env = makeEnvelope({
      verdict: "REQUEST_CHANGES",
      findings: [
        {
          id: "sha256:f1",
          severity: "high",
          check: "c",
          rationale: "r",
          required_action: "f",
          location: { file: null, line: null, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    const decision = routeVerdict(env, makeContext());
    const comment = buildAuditComment({
      iterationNumber: 2,
      envelope: env,
      decision,
      fixTaskIdentifier: "DRV-99",
      fixTaskId: "task-001-uuid",
    });
    expect(comment).toContain("[DRV-99](mention://issue/task-001-uuid)");
    expect(comment).not.toMatch(/mention:\/\/agent/);
  });

  it("ESCALATE: contains escalation reason", () => {
    const env = makeEnvelope({
      verdict: "BLOCKER",
      findings: [
        {
          id: "sha256:sc1",
          severity: "critical",
          check: "scope-change",
          rationale: "Scope change.",
          required_action: "Human must decide.",
          location: { file: null, line: null, range: null, commit_sha: "abc1234" },
          tags: [],
        },
      ],
    });
    const decision = routeVerdict(env, makeContext());
    const comment = buildAuditComment({ iterationNumber: 1, envelope: env, decision });
    expect(comment).toContain("Escalated");
    expect(comment).toContain("Human decision required");
    expect(comment).not.toMatch(/mention:\/\/agent/);
  });

  it("no agent mention links in any audit comment", () => {
    const scenarios: Array<[ReturnType<typeof makeEnvelope>, RoutingContext]> = [
      [makeEnvelope({ verdict: "APPROVED" }), makeContext()],
      [makeEnvelope({ verdict: "WARNING" }), makeContext()],
      [
        makeEnvelope({
          verdict: "REQUEST_CHANGES",
          findings: [
            {
              id: "sha256:f",
              severity: "high",
              check: "c",
              rationale: "r",
              required_action: "f",
              location: { file: null, line: null, range: null, commit_sha: "abc1234" },
              tags: [],
            },
          ],
        }),
        makeContext(),
      ],
      [
        makeEnvelope({
          verdict: "BLOCKER",
          findings: [
            {
              id: "sha256:sc",
              severity: "critical",
              check: "scope-change",
              rationale: ".",
              required_action: "Human.",
              location: { file: null, line: null, range: null, commit_sha: "abc1234" },
              tags: [],
            },
          ],
        }),
        makeContext(),
      ],
    ];
    for (const [env, ctx] of scenarios) {
      const decision = routeVerdict(env, ctx);
      const comment = buildAuditComment({ iterationNumber: 1, envelope: env, decision });
      expect(comment).not.toMatch(/mention:\/\/agent/);
    }
  });
});
