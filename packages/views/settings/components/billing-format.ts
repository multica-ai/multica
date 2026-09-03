const STRIPE_ZERO_DECIMAL_CURRENCIES = new Set([
  "BIF",
  "CLP",
  "DJF",
  "GNF",
  "JPY",
  "KMF",
  "KRW",
  "MGA",
  "PYG",
  "RWF",
  "VND",
  "VUV",
  "XAF",
  "XOF",
  "XPF",
]);
const STRIPE_TWO_DECIMAL_COMPAT_CURRENCIES = new Set(["ISK", "UGX"]);
const STRIPE_THREE_DECIMAL_CURRENCIES = new Set([
  "BHD",
  "JOD",
  "KWD",
  "OMR",
  "TND",
]);

/** Format a Stripe minor-unit amount without guessing currency precision. */
export function formatStripeMinorAmount(
  amount: number,
  currency: string,
  locale: string,
): string | null {
  if (!Number.isSafeInteger(amount) || amount < 0) return null;
  const normalizedCurrency = currency.trim().toUpperCase();
  if (!normalizedCurrency) return null;

  try {
    const fractionDigits = STRIPE_TWO_DECIMAL_COMPAT_CURRENCIES.has(
      normalizedCurrency,
    )
      ? 2
      : STRIPE_ZERO_DECIMAL_CURRENCIES.has(normalizedCurrency)
        ? 0
        : STRIPE_THREE_DECIMAL_CURRENCIES.has(normalizedCurrency)
          ? 3
          : 2;
    const majorAmount = amount / 10 ** fractionDigits;
    const showStripeFraction = !Number.isInteger(majorAmount);
    return new Intl.NumberFormat(locale, {
      style: "currency",
      currency: normalizedCurrency,
      ...(showStripeFraction
        ? {
            minimumFractionDigits: fractionDigits,
            maximumFractionDigits: fractionDigits,
          }
        : {}),
    }).format(majorAmount);
  } catch {
    return null;
  }
}
