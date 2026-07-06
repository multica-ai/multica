import type { Decision, EvalContext, Provider } from "./types";

/**
 * FeatureFlagService is the framework-level Toggle Router. UI code asks the
 * Service for decisions; the Service consults its configured {@link Provider}.
 *
 * The class is intentionally side-effect free. Mounting it inside a React
 * tree is handled by `./context.tsx`; the Service itself works outside of
 * React (unit tests, web workers, Node CLI tools, ...).
 *
 * Always-on safety: every public entry point returns the caller's default
 * when no provider matches. Business code never has to guard against a
 * missing flag.
 */
export class FeatureFlagService {
  private provider: Provider | null;

  constructor(provider: Provider | null = null) {
    this.provider = provider;
  }

  /**
   * Swap the underlying provider at runtime. Useful when fresh config
   * arrives from the backend; the React provider tree re-renders
   * automatically because the consumer hooks subscribe to the wrapper.
   */
  setProvider(provider: Provider | null): void {
    this.provider = provider;
  }

  /**
   * Returns true when the named flag evaluates to an "on" state. When the
   * flag is unknown the caller's default is returned.
   *
   * @example
   *   if (flags.isEnabled("billing_new_invoice_email", { userId }, false)) {
   *     return <NewInvoiceEmail />;
   *   }
   *   return <LegacyInvoiceEmail />;
   */
  isEnabled(key: string, ctx: EvalContext, defaultValue: boolean): boolean {
    return this.decision(key, ctx, defaultValue).enabled;
  }

  /**
   * Returns the raw variant for a multi-arm flag, falling back to
   * `defaultValue` when nothing matches.
   */
  variant(key: string, ctx: EvalContext, defaultValue: string): string {
    if (!this.provider) {
      return defaultValue;
    }
    const d = this.provider.lookup(key, ctx);
    if (!d) return defaultValue;
    return d.variant;
  }

  /**
   * Full structured decision. Used by diagnostic overlays and tests.
   */
  decision(key: string, ctx: EvalContext, defaultValue: boolean): Decision {
    if (!this.provider) {
      return defaultDecision(key, defaultValue);
    }
    const d = this.provider.lookup(key, ctx);
    if (!d) return defaultDecision(key, defaultValue);
    return { ...d, key };
  }

  /** Returns the wrapped provider (read-only) for diagnostics. */
  getProvider(): Provider | null {
    return this.provider;
  }
}

function defaultDecision(key: string, value: boolean): Decision {
  return {
    key,
    enabled: value,
    variant: value ? "on" : "off",
    reason: "default",
    source: "default",
  };
}
