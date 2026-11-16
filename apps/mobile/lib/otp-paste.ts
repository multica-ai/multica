/** Native input events can batch several typed digits. Keep partial values;
 * the dependency's full-code-only paste default would erase those batches. */
export function transformOtpPaste(text: string, digits: number): string {
  if (/^\d*$/.test(text) && text.length <= digits) return text;
  return text.match(new RegExp(`(?:^|\\D)(\\d{${digits}})(?:\\D|$)`))?.[1] ?? "";
}
