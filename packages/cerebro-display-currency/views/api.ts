"use client";

import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  DEFAULT_DISPLAY_CURRENCY,
  type DisplayCurrency,
  formatCostCents,
  formatCostUsd,
  normalizeCurrency,
} from "@multica/cerebro-format-cost";

const displayCurrencyKeys = {
  all: (wsId: string) => ["cerebro-display-currency", wsId] as const,
};

// The GET /display-currency response. Every field optional/nullable so a drifted
// backend degrades to "USD, no rate" instead of throwing into the UI.
const responseSchema = z.object({
  currency: z.string(),
  base: z.string().optional(),
  rate: z.number().nullable().optional(),
  fetched_at: z.string().nullable().optional(),
});

type DisplayCurrencyResponse = z.infer<typeof responseSchema>;

const FALLBACK: DisplayCurrencyResponse = {
  currency: DEFAULT_DISPLAY_CURRENCY,
  base: "USD",
  rate: null,
  fetched_at: null,
};

/**
 * Raw query for the workspace's display currency + cached USD->currency rate.
 * Accepts an explicit `wsId` so it can run outside a WorkspaceIdProvider;
 * defaults to the current workspace context.
 */
export function useDisplayCurrencyQuery(wsId?: string) {
  // useCurrentWorkspace() is null-safe (unlike useWorkspaceId, which throws
  // outside a workspace route) so the cost formatter can mount anywhere — e.g.
  // the chat cost chip deep in a panel. No workspace -> id "" -> query disabled
  // -> defaults to USD.
  const ws = useCurrentWorkspace();
  const id = wsId ?? ws?.id ?? "";
  return useQuery({
    queryKey: displayCurrencyKeys.all(id),
    enabled: Boolean(id),
    queryFn: async (): Promise<DisplayCurrencyResponse> =>
      parseWithFallback(
        await api.cerebroRequest(`/api/workspaces/${id}/display-currency`),
        responseSchema,
        FALLBACK,
        { endpoint: "GET /api/workspaces/:id/display-currency" },
      ),
  });
}

export interface DisplayCurrencyState {
  currency: DisplayCurrency;
  /** USD->currency rate, or undefined when USD / not cached yet. */
  rate: number | undefined;
  /** ISO timestamp of the cached rate, for freshness display. */
  fetchedAt: string | undefined;
  isLoading: boolean;
}

/** Resolved display-currency state for the workspace. */
export function useDisplayCurrency(wsId?: string): DisplayCurrencyState {
  const { data, isLoading } = useDisplayCurrencyQuery(wsId);
  return {
    currency: normalizeCurrency(data?.currency),
    rate: data?.rate ?? undefined,
    fetchedAt: data?.fetched_at ?? undefined,
    isLoading,
  };
}

export interface CostFormatter {
  /** Format a cost given in USD cents into the workspace display currency. */
  formatCents: (cents: number | null | undefined) => string;
  /** Format a cost already expressed in USD dollars. */
  formatUsd: (usd: number | null | undefined) => string;
  /** Like formatUsd but drops decimals at or above 100 (KPI tiles). */
  formatUsdCompact: (usd: number | null | undefined) => string;
  currency: DisplayCurrency;
  rate: number | undefined;
  fetchedAt: string | undefined;
}

/**
 * The hook call sites use: returns ready-to-call `formatCents` / `formatUsd`
 * bound to the workspace's currency + rate. Stable across renders for a given
 * (currency, rate).
 */
export function useCostFormatter(wsId?: string): CostFormatter {
  const { currency, rate, fetchedAt } = useDisplayCurrency(wsId);
  return useMemo(() => {
    const opts = { displayCurrency: currency, rate };
    return {
      formatCents: (cents: number | null | undefined) => formatCostCents(cents, opts),
      formatUsd: (usd: number | null | undefined) => formatCostUsd(usd, opts),
      formatUsdCompact: (usd: number | null | undefined) =>
        formatCostUsd(usd, { ...opts, compact: true }),
      currency,
      rate,
      fetchedAt,
    };
  }, [currency, rate, fetchedAt]);
}

/** Set the workspace display currency (admin/owner only on the server). */
export function useSetDisplayCurrencyMutation(wsId?: string) {
  const ws = useCurrentWorkspace();
  const id = wsId ?? ws?.id ?? "";
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (currency: DisplayCurrency) =>
      api.cerebroRequest(`/api/workspaces/${id}/display-currency`, {
        method: "PUT",
        body: JSON.stringify({ currency }),
      }),
    onMutate: async (currency: DisplayCurrency) => {
      await qc.cancelQueries({ queryKey: displayCurrencyKeys.all(id) });
      const previous = qc.getQueryData<DisplayCurrencyResponse>(displayCurrencyKeys.all(id));
      qc.setQueryData<DisplayCurrencyResponse>(displayCurrencyKeys.all(id), (old) => ({
        ...(old ?? FALLBACK),
        currency,
      }));
      return { previous };
    },
    onError: (_err, _currency, context) => {
      if (context?.previous) {
        qc.setQueryData(displayCurrencyKeys.all(id), context.previous);
      }
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: displayCurrencyKeys.all(id) });
    },
  });
}
