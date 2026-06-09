export function formatSlashCommandLabel(label: unknown): string {
  return typeof label === "string" && label.trim().length > 0 ? label : "?";
}
