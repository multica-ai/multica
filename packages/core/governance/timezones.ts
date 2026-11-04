/** Used when the runtime cannot list its timezones (`Intl.supportedValuesOf` is missing). */
export const FALLBACK_TIMEZONES: readonly string[] = [
  "UTC",
  "Europe/London",
  "Europe/Paris",
  "America/New_York",
  "America/Chicago",
  "America/Los_Angeles",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Asia/Seoul",
  "Australia/Sydney",
];

/**
 * The timezones a quiet-hours window can use, sorted, always including
 * `current` so a saved value the list lacks still shows in its select.
 */
export function timezoneOptions(current: string, supported: () => readonly string[] = listSupportedTimezones): string[] {
  const names = new Set(supported());
  if (current) names.add(current);
  return [...names].sort((a, b) => a.localeCompare(b));
}

function listSupportedTimezones(): readonly string[] {
  try {
    const intl = Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] };
    const values = intl.supportedValuesOf?.("timeZone");
    return values && values.length > 0 ? values : FALLBACK_TIMEZONES;
  } catch {
    return FALLBACK_TIMEZONES;
  }
}
