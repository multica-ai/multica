export function splitCustomArgEntry(value: string): string[] {
  return value.trim().split(/\s+/).filter(Boolean);
}
