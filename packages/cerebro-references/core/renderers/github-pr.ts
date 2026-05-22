import { registerObjectRenderer } from "../registry";
import type { IssueReference } from "../types";

// Accepts user-entered identifiers for a GitHub pull request in three
// shapes:
//   1. "org/repo#NNN"      — fully qualified
//   2. "repo#NNN"          — short form; org filled from `defaultOrg`
//   3. "https://github.com/org/repo/pull/NNN" (any http(s) URL works)
// Returns a normalized {org, repo, number, url} tuple, or null when the
// input doesn't match any form. We deliberately do NOT call the GitHub
// API in v1 — parsing is local-only.

export interface GithubPrIdent {
  org: string;
  repo: string;
  number: number;
  url: string;
}

export function parseGithubPr(
  input: string,
  options: { defaultOrg?: string } = {},
): GithubPrIdent | null {
  const trimmed = input.trim();
  if (!trimmed) return null;

  const url = parseGithubPrUrl(trimmed);
  if (url) return url;

  const hashIdx = trimmed.indexOf("#");
  if (hashIdx < 0) return null;
  const left = trimmed.slice(0, hashIdx).trim();
  const right = trimmed.slice(hashIdx + 1).trim();
  if (!left || !right) return null;
  const number = parseStrictPositiveInt(right);
  if (number == null) return null;

  if (left.includes("/")) {
    const parts = left.split("/");
    if (parts.length !== 2) return null;
    const [org, repo] = parts;
    if (!org || !repo) return null;
    return {
      org,
      repo,
      number,
      url: `https://github.com/${org}/${repo}/pull/${number}`,
    };
  }

  const org = options.defaultOrg?.trim();
  if (!org) return null;
  return {
    org,
    repo: left,
    number,
    url: `https://github.com/${org}/${left}/pull/${number}`,
  };
}

function parseGithubPrUrl(input: string): GithubPrIdent | null {
  let url: URL;
  try {
    url = new URL(input);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return null;
  if (url.hostname !== "github.com" && url.hostname !== "www.github.com") {
    return null;
  }
  const segments = url.pathname.split("/").filter(Boolean);
  if (segments.length < 4) return null;
  const org = segments[0];
  const repo = segments[1];
  const kind = segments[2];
  const numberRaw = segments[3];
  if (!org || !repo || !numberRaw) return null;
  if (kind !== "pull" && kind !== "pulls") return null;
  const number = parseStrictPositiveInt(numberRaw);
  if (number == null) return null;
  return {
    org,
    repo,
    number,
    url: `https://github.com/${org}/${repo}/pull/${number}`,
  };
}

function parseStrictPositiveInt(raw: string): number | null {
  if (!/^[0-9]+$/.test(raw)) return null;
  const n = Number(raw);
  if (!Number.isInteger(n) || n <= 0) return null;
  return n;
}

// Surfaces the PR's state from metadata in a human-readable form. The
// backend stores whatever the caller posted under `metadata.state`; we
// recognise the common GitHub states and pass anything else through as-is
// so future states render without a code change.
const KNOWN_STATES: Record<string, string> = {
  open: "Open",
  merged: "Merged",
  closed: "Closed",
  draft: "Draft",
};

export function formatGithubPrBadge(ref: IssueReference): string | null {
  const raw = ref.metadata?.state;
  if (typeof raw !== "string" || raw.trim() === "") return null;
  const key = raw.trim().toLowerCase();
  return KNOWN_STATES[key] ?? raw.trim();
}

export function formatGithubPrTitle(ref: IssueReference): string {
  if (ref.label && ref.label.trim() !== "") return ref.label;
  const parsed = parseGithubPr(ref.ref_id);
  if (parsed) return `${parsed.org}/${parsed.repo}#${parsed.number}`;
  return ref.ref_id;
}

export function resolveGithubPrUrl(ref: IssueReference): string | null {
  if (ref.url && ref.url.trim() !== "") return ref.url;
  const parsed = parseGithubPr(ref.ref_id);
  return parsed ? parsed.url : null;
}

registerObjectRenderer({
  object: "github_pr",
  formatTitle: formatGithubPrTitle,
  formatBadge: formatGithubPrBadge,
  resolveUrl: resolveGithubPrUrl,
});
