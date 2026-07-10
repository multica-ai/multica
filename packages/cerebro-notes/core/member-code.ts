// FIR-2810: memberCode derives the short author code stamped on note lines,
// following Firtal's member-code convention: the first two letters of the
// first name plus the first letter of the last name, uppercased —
// "Jesper Hvejsel" → "JEH", "Morten Krøjer Persson" → "MOP". A single-word
// name takes its first three letters. Empty/blank names yield "".
export function memberCode(name: string): string {
  const words = name
    .trim()
    .split(/\s+/)
    .filter((w) => w.length > 0);
  const first = words[0];
  if (!first) return "";
  if (words.length === 1) return first.slice(0, 3).toUpperCase();
  const last = words[words.length - 1] ?? "";
  return (first.slice(0, 2) + last.slice(0, 1)).toUpperCase();
}
