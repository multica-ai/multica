// For GitHub URLs, return "owner/repo" so truncated display labels stay
// meaningful when URLs share a long prefix. Handles:
//   https://github.com/owner/repo[/...]
//   https://www.github.com/owner/repo
//   ssh://git@github.com/owner/repo.git
//   git@github.com:owner/repo.git   (scp shorthand)
// The .git suffix is stripped in all cases. When the resulting label is still
// long, middle-truncation keeps the distinguishing tail visible (the case where
// many repos share a long common prefix, e.g. customer-alpha vs customer-beta).
// Non-GitHub input (enterprise hosts, malformed strings) is returned unchanged.
export function githubShortLabel(url: string): string {
  // scp shorthand — new URL() throws on these, so match before the try block.
  const scp = url.match(/^(?:[^@/]+@)?github\.com:([^/]+)\/([^/]+?)(?:\.git)?$/);
  if (scp) return midTruncate(`${scp[1]}/${scp[2]}`);
  try {
    const u = new URL(url);
    if (u.hostname === "github.com" || u.hostname === "www.github.com") {
      const [owner, repo] = u.pathname.split("/").filter(Boolean);
      if (owner && repo) return midTruncate(`${owner}/${repo.replace(/\.git$/, "")}`);
    }
  } catch {
    // not a parseable URL — fall through and return as-is
  }
  return url;
}

// Middle-truncate a string to at most maxLen characters, preserving both the
// leading and trailing portions. The trailing portion is what distinguishes
// repos that share a long common prefix (e.g. customer-alpha vs customer-beta).
export function midTruncate(s: string, maxLen = 40): string {
  if (s.length <= maxLen) return s;
  const tail = Math.floor((maxLen - 1) / 2);
  const head = maxLen - 1 - tail;
  return `${s.slice(0, head)}…${s.slice(-tail)}`;
}
