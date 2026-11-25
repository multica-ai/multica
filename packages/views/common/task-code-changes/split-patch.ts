/**
 * Split a checkout's `git diff` into per-file sections keyed by the file's
 * path (its old path for a deletion), so the code-changes dialog can show one
 * file at a time.
 */
export function splitPatchByFile(patch: string): Map<string, string> {
  const out = new Map<string, string>();
  if (!patch) return out;
  for (const section of patch.split(/^(?=diff --git )/m)) {
    if (!section.startsWith("diff --git ")) continue;
    const path = sectionPath(section);
    if (path) out.set(path, section);
  }
  return out;
}

function sectionPath(section: string): string | null {
  const lines = section.split("\n", 8);
  // Prefer the explicit headers: `+++ b/<path>`, or `--- a/<path>` with
  // `+++ /dev/null` for a deletion, and `rename to` for a pure rename.
  let minus: string | null = null;
  for (const line of lines) {
    if (line.startsWith("rename to ")) return line.slice("rename to ".length);
    if (line.startsWith("--- ")) minus = stripPrefix(line.slice(4), "a/");
    if (line.startsWith("+++ ")) {
      const plus = line.slice(4);
      if (plus === "/dev/null") return minus;
      return stripPrefix(plus, "b/");
    }
  }
  // Binary or mode-only change: fall back to `diff --git a/<p> b/<p>`.
  const m = /^diff --git a\/(.+) b\/(.+)$/.exec(lines[0] ?? "");
  return m ? m[2]! : null;
}

function stripPrefix(path: string, prefix: string): string {
  const trimmed = path.replace(/\t.*$/, "");
  return trimmed.startsWith(prefix) ? trimmed.slice(prefix.length) : trimmed;
}
