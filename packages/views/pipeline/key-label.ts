/**
 * A server key (a knowledge kind such as `validation_report`, or a stage key
 * such as `validate`) as a readable label: "Validation report", "Validate".
 * The keys are a closed set owned by the server and their names are not
 * translated here, so this is only a readable form of the key.
 */
export function humanizeKey(key: string): string {
  const words = key.replace(/_/g, " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : key;
}
