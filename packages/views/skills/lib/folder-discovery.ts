import type { CreateSkillRequest } from "@multica/core/types";
import { parseFrontmatter } from "./frontmatter";

// Mirror the server caps (server/internal/handler/skill.go).
export const MAX_FILE_SIZE = 1 << 20; // 1 MiB
export const MAX_TOTAL_SIZE = 8 << 20; // 8 MiB per skill
export const MAX_FILE_COUNT = 128; // supporting files per skill
export const MAX_CANDIDATES = 100; // per bulk operation

// Mirror server isLikelyBinaryFilePath extension list.
const BINARY_EXT = new Set([
  "png", "jpg", "jpeg", "gif", "webp", "ico", "bmp", "svg", "pdf", "zip",
  "gz", "tar", "exe", "dll", "so", "dylib", "mp4", "mov", "mp3", "wav",
  "woff", "woff2", "ttf", "otf", "bin", "wasm",
]);

export type FolderEntry = {
  relativePath: string; // webkitRelativePath, e.g. "root/skills/a/SKILL.md"
  size: number;
  text: () => Promise<string>;
};

export type FolderCandidate = {
  name: string;
  description: string;
  path: string; // dir containing SKILL.md
  data: CreateSkillRequest;
};

export type FolderDiscovery = {
  candidates: FolderCandidate[];
  truncated: boolean;
};

function isBinary(path: string): boolean {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  return BINARY_EXT.has(ext);
}

export async function discoverFolderSkills(
  entries: FolderEntry[],
): Promise<FolderDiscovery> {
  // Each SKILL.md anchors a skill at its directory.
  const skillDirs = entries
    .filter(
      (e) =>
        e.relativePath.endsWith("/SKILL.md") || e.relativePath === "SKILL.md",
    )
    .map((e) => e.relativePath.replace(/SKILL\.md$/, "")); // keeps trailing slash or ""

  // Sort longest-prefix-first so a nested skill claims its files before an
  // ancestor skill does.
  const dirsByDepth = [...skillDirs].sort((a, b) => b.length - a.length);

  const candidates: FolderCandidate[] = [];
  const truncated = skillDirs.length > MAX_CANDIDATES;

  for (const dir of skillDirs.slice(0, MAX_CANDIDATES)) {
    const mdEntry = entries.find((e) => e.relativePath === dir + "SKILL.md")!;
    const content = await mdEntry.text();
    const fm = parseFrontmatter(content);
    const dirName = dir.replace(/\/$/, "").split("/").pop() || "skill";
    const name = fm.name || dirName;

    const files: { path: string; content: string }[] = [];
    let total = content.length;
    for (const e of entries) {
      if (!e.relativePath.startsWith(dir)) continue;
      if (e.relativePath === dir + "SKILL.md") continue;
      // Claim a file only if THIS dir is its nearest SKILL.md ancestor.
      const nearest = dirsByDepth.find((d) => e.relativePath.startsWith(d));
      if (nearest !== dir) continue;
      const rel = e.relativePath.slice(dir.length);
      if (isBinary(rel)) continue;
      if (e.size > MAX_FILE_SIZE) continue;
      if (files.length >= MAX_FILE_COUNT) continue;
      if (total + e.size > MAX_TOTAL_SIZE) continue;
      files.push({ path: rel, content: await e.text() });
      total += e.size;
    }

    candidates.push({
      name,
      description: fm.description,
      path: dir.replace(/\/$/, "") || "(root)",
      data: { name, description: fm.description, content, files },
    });
  }

  return { candidates, truncated };
}
