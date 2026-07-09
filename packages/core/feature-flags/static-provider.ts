import type { Decision, EvalContext, Provider, Rule } from "./types";
import { inPercent } from "./hash";

/**
 * StaticProvider is an in-memory Provider populated either programmatically
 * or from a JSON config shipped with the application bundle.
 *
 * This is the recommended baseline provider for the frontend: configuration
 * lives in source control, moves through CD alongside the build, and
 * changes require a deploy. For dynamic flags fetched from the backend,
 * wrap a {@link StaticProvider} behind a chain provider that also reads
 * from API state — the StaticProvider then acts as a safety net for the
 * very first paint before the API response is available.
 */
export class StaticProvider implements Provider {
  readonly name = "static";
  private rules: Map<string, Rule>;

  constructor(rules: Readonly<Record<string, Rule>> = {}) {
    this.rules = new Map(Object.entries(rules));
  }

  /** Replace or install the rule for `key`. */
  set(key: string, rule: Rule): void {
    this.rules.set(key, rule);
  }

  /**
   * Replace every rule atomically. Use when reloading flag config from a
   * fetch response so consumers never observe a mixed state.
   */
  loadRules(rules: Readonly<Record<string, Rule>>): void {
    this.rules = new Map(Object.entries(rules));
  }

  /** Sorted list of known flag keys. Useful for dev overlays. */
  keys(): string[] {
    return Array.from(this.rules.keys()).sort();
  }

  lookup(key: string, ctx: EvalContext): Decision | undefined {
    const rule = this.rules.get(key);
    if (!rule) return undefined;
    return evaluateRule(key, rule, ctx);
  }
}

function evaluateRule(key: string, rule: Rule, ctx: EvalContext): Decision {
  // Deny wins over everything else; a kill switch must remain reachable
  // even when other targeting matches.
  const denyBy = rule.denyBy ?? "user_id";
  if (rule.deny && rule.deny.length > 0) {
    const v = lookupAttr(ctx, denyBy);
    if (v && rule.deny.includes(v)) {
      return decisionFromRule(key, rule, false, "static");
    }
  }

  const allowBy = rule.allowBy ?? "user_id";
  if (rule.allow && rule.allow.length > 0) {
    const v = lookupAttr(ctx, allowBy);
    if (v && rule.allow.includes(v)) {
      return decisionFromRule(key, rule, true, "static");
    }
  }

  if (rule.percent) {
    const by = rule.percent.by ?? "user_id";
    const ident = lookupAttr(ctx, by) ?? "";
    const enabled = inPercent(key, ident, rule.percent.percent);
    return decisionFromRule(key, rule, enabled, "percent");
  }

  return decisionFromRule(key, rule, rule.default ?? false, "static");
}

function decisionFromRule(
  key: string,
  rule: Rule,
  enabled: boolean,
  reason: Decision["reason"],
): Decision {
  // Variant policy: rule.variant is the ON-variant. When the rule
  // evaluates to false we return the canonical "off" so a caller
  // branching on the variant cannot accidentally enter the experiment
  // arm for a user that did not roll in.
  let variant = boolToVariant(enabled);
  if (enabled && rule.variant && rule.variant.length > 0) {
    variant = rule.variant;
  }
  return {
    key,
    enabled,
    variant,
    reason,
    source: "static",
  };
}

function boolToVariant(b: boolean): string {
  return b ? "on" : "off";
}

/**
 * Resolve an attribute name against the EvalContext. The well-known names
 * "user_id" and "workspace_id" map to the dedicated fields so rules can use
 * them by name without callers also populating `attributes`.
 */
function lookupAttr(ctx: EvalContext, name: string): string | undefined {
  if (name === "user_id") return nonEmpty(ctx.userId);
  if (name === "workspace_id") return nonEmpty(ctx.workspaceId);
  return nonEmpty(ctx.attributes?.[name]);
}

function nonEmpty(v: string | undefined): string | undefined {
  return v && v.length > 0 ? v : undefined;
}
