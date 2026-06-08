/**
 * @multica/cerebro-format-cost — the single source of truth for turning a
 * USD cost into a display string.
 *
 * Why this is its own zero-dependency package (FIR-40): cost is formatted in
 * ~11 places today, split across two package zones. The chat components
 * (`@multica/cerebro-chat`) deliberately kept their formatters local to avoid a
 * circular edge into `@multica/views` (the JEH-736 note). A tiny leaf package
 * that BOTH `@multica/views` and `@multica/cerebro-chat` can import sidesteps
 * that cycle entirely.
 *
 * Design principle: amounts are always stored in USD (cents). Currency is a
 * pure DISPLAY concern — we convert at render time with a cached daily rate, so
 * historical cost numbers are never bound to a particular day's rate and the
 * display currency can change with no data migration.
 */

/** Currencies supported in the first delivery (FIR-40). */
export type DisplayCurrency = "USD" | "DKK" | "EUR";

export const SUPPORTED_CURRENCIES: readonly DisplayCurrency[] = [
  "USD",
  "DKK",
  "EUR",
];

/** Default display currency — preserves today's behavior (raw USD). */
export const DEFAULT_DISPLAY_CURRENCY: DisplayCurrency = "USD";

/**
 * A cached USD->target exchange rate plus when it was fetched, so callers can
 * surface freshness. `rate` is "target units per 1 USD" (USD itself is 1).
 */
export interface ExchangeRate {
  /** Target currency this rate converts USD into. */
  currency: DisplayCurrency;
  /** Target-currency units per 1 USD. Must be > 0. */
  rate: number;
  /** ISO-8601 timestamp of when the rate was fetched, if known. */
  fetchedAt?: string;
}

export interface FormatCostOptions {
  /** Display currency. Defaults to USD (unchanged, raw-USD behavior). */
  displayCurrency?: DisplayCurrency;
  /**
   * USD->displayCurrency rate (target units per 1 USD). Required when
   * displayCurrency !== "USD". Ignored for USD. A missing/invalid rate for a
   * non-USD currency falls back to rendering plain USD, so the UI never breaks
   * just because today's rate hasn't been fetched yet.
   */
  rate?: number;
  /**
   * BCP-47 locale for Intl formatting. Defaults to a sensible per-currency
   * locale (USD->en-US, DKK/EUR->da-DK, matching our Danish users).
   */
  locale?: string;
  /**
   * Compact mode for large KPI figures: drop the fractional part once the
   * displayed amount is >= 100 (e.g. "$1,234" instead of "$1,234.00"). Mirrors
   * the dashboard/usage-tile `fmtMoney` rounding so the USD default is byte-for-
   * byte unchanged.
   */
  compact?: boolean;
}

const DEFAULT_LOCALE: Record<DisplayCurrency, string> = {
  USD: "en-US",
  DKK: "da-DK",
  EUR: "da-DK",
};

/** True when `c` is one of the supported display currencies. */
export function isSupportedCurrency(c: string): c is DisplayCurrency {
  return (SUPPORTED_CURRENCIES as readonly string[]).includes(c);
}

/**
 * Coerce arbitrary input (e.g. a workspace setting that drifted) into a valid
 * display currency, falling back to USD. Enum drift downgrades, never throws.
 */
export function normalizeCurrency(c: string | null | undefined): DisplayCurrency {
  if (c && isSupportedCurrency(c)) return c;
  return DEFAULT_DISPLAY_CURRENCY;
}

function resolve(options?: FormatCostOptions): {
  currency: DisplayCurrency;
  rate: number;
  locale: string;
} {
  const currency = normalizeCurrency(options?.displayCurrency);
  if (currency === "USD") {
    return { currency, rate: 1, locale: options?.locale ?? DEFAULT_LOCALE.USD };
  }
  const rate = options?.rate;
  // No usable rate yet -> degrade gracefully to USD rather than show a wrong
  // or NaN amount. Callers that care can check rate freshness separately.
  if (typeof rate !== "number" || !Number.isFinite(rate) || rate <= 0) {
    return { currency: "USD", rate: 1, locale: options?.locale ?? DEFAULT_LOCALE.USD };
  }
  return { currency, rate, locale: options?.locale ?? DEFAULT_LOCALE[currency] };
}

function format(amount: number, currency: DisplayCurrency, locale: string, compact: boolean): string {
  // Compact tiles drop decimals once the figure is >= 100 (mirrors fmtMoney).
  const digits = compact && Math.abs(amount) >= 100 ? 0 : 2;
  const fmt = new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });

  if (amount === 0) return fmt.format(0);

  // Preserve the "less than the smallest visible amount" affordance the inline
  // formatters had ("<$0.01"), now currency-aware.
  const smallest = 0.01;
  if (Math.abs(amount) < smallest) {
    return `<${fmt.format(amount < 0 ? -smallest : smallest)}`;
  }
  return fmt.format(amount);
}

/**
 * Format a cost given in USD CENTS into the chosen display currency.
 * This is the primary entry point — most call sites hold integer cents
 * (`cost_cents`).
 */
export function formatCostCents(
  cents: number | null | undefined,
  options?: FormatCostOptions,
): string {
  const safeCents = typeof cents === "number" && Number.isFinite(cents) ? cents : 0;
  return formatCostUsd(safeCents / 100, options);
}

/**
 * Format a cost already expressed in USD (dollars) into the chosen display
 * currency. Use this at the sites that pre-aggregate to USD (charts, KPI
 * tiles, `estimateCost`).
 */
export function formatCostUsd(
  usd: number | null | undefined,
  options?: FormatCostOptions,
): string {
  const safeUsd = typeof usd === "number" && Number.isFinite(usd) ? usd : 0;
  const { currency, rate, locale } = resolve(options);
  return format(safeUsd * rate, currency, locale, options?.compact ?? false);
}

export interface FormattedCost {
  /** Primary, converted + localized string (e.g. "4,10 kr."). */
  display: string;
  /** Always-USD string for traceability back to billing (e.g. "$0.60"). */
  originalUsd: string;
  /** Whether `display` is a non-USD conversion (false when degraded to USD). */
  converted: boolean;
}

/**
 * Like {@link formatCostCents} but also returns the original USD string and
 * whether a conversion actually happened — for the "≈ 4,10 kr. ($0,60)"
 * tooltip/parenthetical the spec requires for billing traceability.
 */
export function formatCostWithOriginal(
  cents: number | null | undefined,
  options?: FormatCostOptions,
): FormattedCost {
  const { currency, rate } = resolve(options);
  const display = formatCostCents(cents, options);
  const originalUsd = formatCostCents(cents, { displayCurrency: "USD" });
  return {
    display,
    originalUsd,
    converted: currency !== "USD" && rate !== 1,
  };
}
