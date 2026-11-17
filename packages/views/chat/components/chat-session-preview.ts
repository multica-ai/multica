// IM-style timestamp: today → clock, this year → M/D, else full date.
export function formatChatTime(dateStr: string, locale: string): string {
  const d = new Date(dateStr);
  const now = new Date();
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" });
  }
  if (d.getFullYear() === now.getFullYear()) {
    return d.toLocaleDateString(locale, { month: "numeric", day: "numeric" });
  }
  return d.toLocaleDateString(locale);
}

// Collapse a (possibly markdown / multi-line) message into a one-line preview.
export function toPreview(content: string): string {
  return content
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/[#*`>~]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}
