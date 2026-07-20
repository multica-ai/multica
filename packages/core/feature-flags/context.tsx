"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
import type { EvalContext } from "./types";
import { FeatureFlagService } from "./service";

/**
 * React glue for the FeatureFlagService.
 *
 * Two pieces are exported:
 *
 *  - {@link FeatureFlagsProvider}: wraps a part of the tree with a Service
 *    and an EvalContext. The Service is usually constructed once at the
 *    application root; the EvalContext changes as the user context changes
 *    (e.g. after login).
 *  - {@link useFlag} / {@link useVariant}: the recommended Toggle Points in
 *    UI code. They never throw; if the provider tree is missing they fall
 *    back to the supplied default, which keeps Storybook stories and unit
 *    tests from needing to mount the provider just to render a button.
 *
 * Note: we deliberately do NOT expose the underlying FeatureFlagService
 * through hooks. Components that need raw access can read it via the
 * exported context object, but at the cost of giving up the always-on
 * safety guarantee.
 */

interface FeatureFlagContextValue {
  service: FeatureFlagService;
  ctx: EvalContext;
}

const FeatureFlagContext = createContext<FeatureFlagContextValue | null>(null);

export interface FeatureFlagsProviderProps {
  service: FeatureFlagService;
  /**
   * Targeting context for every flag evaluation inside this subtree.
   * Pass an empty object when the user is anonymous — percent rollouts
   * and allow/deny lists then evaluate against the empty identifier,
   * which is the desired behavior for anonymous traffic.
   */
  context?: EvalContext;
  children: ReactNode;
}

/**
 * Mount a FeatureFlagService and EvalContext into the tree. Replacing the
 * `service` prop on a re-render is allowed but rare; prefer mutating the
 * provider on the existing Service via `setProvider`, which avoids forcing
 * every consumer to re-evaluate.
 */
export function FeatureFlagsProvider({
  service,
  context: ctx = {},
  children,
}: FeatureFlagsProviderProps) {
  const value = useMemo<FeatureFlagContextValue>(
    () => ({ service, ctx }),
    [service, ctx],
  );
  return (
    <FeatureFlagContext.Provider value={value}>{children}</FeatureFlagContext.Provider>
  );
}

/**
 * useFlag returns the boolean state of a feature flag.
 *
 * Outside a {@link FeatureFlagsProvider} the hook returns `defaultValue`,
 * never throws. This keeps tests and stories independent of the provider.
 *
 * @example
 *   const showNewBilling = useFlag("billing_v2_dashboard", false);
 *   return showNewBilling ? <BillingV2 /> : <BillingV1 />;
 */
export function useFlag(key: string, defaultValue: boolean): boolean {
  const value = useContext(FeatureFlagContext);
  if (!value) return defaultValue;
  return value.service.isEnabled(key, value.ctx, defaultValue);
}

/**
 * useVariant returns the raw variant identifier for a multi-arm flag, with
 * the same out-of-provider safety as {@link useFlag}.
 *
 * @example
 *   const variant = useVariant("checkout_algo", "control");
 *   switch (variant) {
 *     case "experiment-v2": return <CheckoutV2 />;
 *     case "experiment-v3": return <CheckoutV3 />;
 *     default:              return <CheckoutControl />;
 *   }
 */
export function useVariant(key: string, defaultValue: string): string {
  const value = useContext(FeatureFlagContext);
  if (!value) return defaultValue;
  return value.service.variant(key, value.ctx, defaultValue);
}

/**
 * Escape hatch for diagnostic overlays that need direct Service access.
 * Returns `null` outside a provider so callers must guard explicitly —
 * this is intentional: random component code should use {@link useFlag},
 * not the raw Service.
 */
export function useFeatureFlagService(): FeatureFlagService | null {
  return useContext(FeatureFlagContext)?.service ?? null;
}
