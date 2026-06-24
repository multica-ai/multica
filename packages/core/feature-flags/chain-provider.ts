import type { Decision, EvalContext, Provider } from "./types";

/**
 * ChainProvider composes multiple providers and returns the first match.
 *
 * Order from most-specific to most-generic: per-request override, server
 * push, static config. The first provider that returns a Decision wins, so
 * the chain naturally implements the "ops override beats static config"
 * pattern callers expect.
 *
 * A ChainProvider that wraps zero providers is valid and always returns
 * undefined, so the Service falls back to the caller's default.
 */
export class ChainProvider implements Provider {
  readonly name = "chain";
  private readonly providers: ReadonlyArray<Provider>;

  constructor(providers: ReadonlyArray<Provider | null | undefined>) {
    // Filter nullish entries so callers can pass optional providers
    // directly: `new ChainProvider([envOverride, baseStatic])`.
    this.providers = providers.filter((p): p is Provider => p != null);
  }

  lookup(key: string, ctx: EvalContext): Decision | undefined {
    for (const p of this.providers) {
      const d = p.lookup(key, ctx);
      if (d !== undefined) return d;
    }
    return undefined;
  }
}
