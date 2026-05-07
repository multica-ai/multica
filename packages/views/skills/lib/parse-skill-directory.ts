import type { CreateSkillRequest } from "@multica/core/types";

interface ParsedFile {
  relativePath: string;
  content: string;
}

/**
 * Parse a FileList (from <input webkitdirectory>) into CreateSkillRequest[].
 *
 * Supported formats:
 * 1. Claude Code: .claude/commands/*.md — each file = one skill
 * 2. Codex: AGENTS.md or codex.md at root = one skill
 * 3. SKILL.md directories: any dir with SKILL.md = one skill
 * 4. Fallback: root-level *.md files treated as individual skills
 */
export async function parseSkillDirectory(files: FileList): Promise<CreateSkillRequest[]> {
  const parsed = await readAllFiles(files);

  if (parsed.length === 0) return [];

  // Try Claude Code format: .claude/commands/*.md
  const claudeSkills = parseClaudeCodeFormat(parsed);
  if (claudeSkills.length > 0) return claudeSkills;

  // Try Codex format: root-level AGENTS.md or codex.md
  const codexSkill = parseCodexFormat(parsed);
  if (codexSkill) return [codexSkill];

  // Try SKILL.md directory format
  const dirSkills = parseSkillMdFormat(parsed);
  if (dirSkills.length > 0) return dirSkills;

  // Fallback: each root .md file = one skill
  return parseFallbackFormat(parsed);
}

// --- File reading ---

async function readAllFiles(files: FileList): Promise<ParsedFile[]> {
  const results: ParsedFile[] = [];
  // Read only text-based files, skip binaries
  const textExtensions = new Set([
    ".md", ".txt", ".yaml", ".yml", ".json", ".toml",
    ".ts", ".tsx", ".js", ".jsx", ".py", ".go", ".rs",
    ".sh", ".bash", ".zsh", ".css", ".html", ".xml",
    ".sql", ".proto", ".graphql",
  ]);

  const promises: Promise<void>[] = [];

  // Determine the top-level directory name from the first file's webkitRelativePath.
  // If it starts with '.' (e.g. .claude, .codex), preserve it in paths so format parsers
  // can identify the skill format by the directory name (e.g. .claude/commands/*.md).
  const topDir = files.length > 0 ? getFirstSegment(files[0]!.webkitRelativePath) : "";
  const preserveTopDir = topDir.startsWith(".");

  for (let i = 0; i < files.length; i++) {
    const file = files[i]!;
    const ext = getExtension(file.name);
    if (!textExtensions.has(ext)) continue;
    if (file.size > 1024 * 1024) continue; // skip files > 1MB

    promises.push(
      file.text().then((content) => {
        // webkitRelativePath is like "dirname/subdir/file.md"
        const relativePath = file.webkitRelativePath;
        // Strip the top-level directory name, except for dot directories whose
        // name is meaningful to format detection (e.g. .claude/commands/).
        const stripped = preserveTopDir ? relativePath : stripFirstSegment(relativePath);
        if (stripped) {
          results.push({ relativePath: stripped, content });
        }
      }),
    );
  }

  await Promise.all(promises);
  return results;
}

// --- Format parsers ---

function parseClaudeCodeFormat(files: ParsedFile[]): CreateSkillRequest[] {
  const commandFiles = files.filter(
    (f) => f.relativePath.startsWith(".claude/commands/") && f.relativePath.endsWith(".md"),
  );

  if (commandFiles.length === 0) return [];

  return commandFiles.map((f) => {
    const filename = f.relativePath.replace(".claude/commands/", "");
    const { name, description } = parseFrontmatter(f.content);
    return {
      name: name || removeExtension(filename),
      description: description || "",
      content: f.content,
    };
  });
}

function parseCodexFormat(files: ParsedFile[]): CreateSkillRequest | null {
  // Match AGENTS.md/codex.md at root, or directly inside a dot directory (e.g. .codex/AGENTS.md).
  const codexFile = files.find(
    (f) =>
      f.relativePath === "AGENTS.md" ||
      f.relativePath === "codex.md" ||
      /^\.[^/]+\/AGENTS\.md$/.test(f.relativePath) ||
      /^\.[^/]+\/codex\.md$/.test(f.relativePath),
  );
  if (!codexFile) return null;

  const { name, description } = parseFrontmatter(codexFile.content);

  // If AGENTS.md is inside a dot directory, only include sibling files under the same prefix.
  const filePrefix = codexFile.relativePath.includes("/")
    ? codexFile.relativePath.slice(0, codexFile.relativePath.lastIndexOf("/") + 1)
    : "";
  const supportingFiles = files.filter(
    (f) =>
      f !== codexFile &&
      (filePrefix === "" || f.relativePath.startsWith(filePrefix)),
  );

  return {
    name: name || "AGENTS",
    description: description || "Imported from Codex format",
    content: codexFile.content,
    files: supportingFiles.map((f) => ({
      path: filePrefix ? f.relativePath.slice(filePrefix.length) : f.relativePath,
      content: f.content,
    })),
  };
}

function parseSkillMdFormat(files: ParsedFile[]): CreateSkillRequest[] {
  // Find all directories that contain a SKILL.md (including root "")
  const skillDirs = new Set<string>();
  for (const f of files) {
    if (!f.relativePath.endsWith("SKILL.md")) continue;
    const dir = getDirectory(f.relativePath);
    skillDirs.add(dir);
  }

  if (skillDirs.size === 0) return [];

  const skills: CreateSkillRequest[] = [];

  for (const dir of skillDirs) {
    const skillMdPath = dir ? `${dir}/SKILL.md` : "SKILL.md";
    const skillMd = files.find((f) => f.relativePath === skillMdPath);
    if (!skillMd) continue;

    const { name, description } = parseFrontmatter(skillMd.content);
    const prefix = dir ? dir + "/" : "";
    const supportingFiles = files.filter(
      (f) => f.relativePath !== skillMdPath && (prefix === "" || f.relativePath.startsWith(prefix)),
    );

    // For root-level SKILL.md, only include files not claimed by other skill dirs
    const filteredFiles = prefix === ""
      ? supportingFiles.filter((f) => {
          const fDir = getDirectory(f.relativePath);
          return !skillDirs.has(fDir);
        })
      : supportingFiles;

    const dirName = dir ? (dir.includes("/") ? dir.split("/").pop()! : dir) : "";

    skills.push({
      name: name || dirName,
      description: description || "",
      content: skillMd.content,
      files: filteredFiles.map((f) => ({
        path: prefix ? f.relativePath.slice(prefix.length) : f.relativePath,
        content: f.content,
      })),
    });
  }

  return skills;
}

function parseFallbackFormat(files: ParsedFile[]): CreateSkillRequest[] {
  // If all files share a single dot-directory prefix (e.g. user uploaded .claude or .mytools),
  // work within that prefix so root .md files are detected correctly.
  const normalized = normalizeDotDirFiles(files);

  const rootMdFiles = normalized.filter(
    (f) => !f.relativePath.includes("/") && f.relativePath.endsWith(".md"),
  );

  if (rootMdFiles.length === 0) {
    // No root .md files — package everything as a single skill
    return [{
      name: "Imported Skills",
      description: "",
      content: normalized.find((f) => f.relativePath.endsWith(".md"))?.content || "",
      files: normalized
        .filter((f) => !f.relativePath.endsWith(".md"))
        .map((f) => ({ path: f.relativePath, content: f.content })),
    }];
  }

  return rootMdFiles.map((f) => {
    const { name, description } = parseFrontmatter(f.content);
    return {
      name: name || removeExtension(f.relativePath),
      description: description || "",
      content: f.content,
    };
  });
}

/** Strip a shared dot-directory prefix when all files live under a single dot directory. */
function normalizeDotDirFiles(files: ParsedFile[]): ParsedFile[] {
  if (files.length === 0) return files;
  const topDirs = new Set(files.map((f) => getFirstSegment(f.relativePath)));
  if (topDirs.size !== 1) return files;
  const [topDir] = topDirs;
  if (!topDir || !topDir.startsWith(".")) return files;
  const prefix = topDir + "/";
  return files.map((f) => ({
    ...f,
    relativePath: f.relativePath.startsWith(prefix)
      ? f.relativePath.slice(prefix.length)
      : f.relativePath,
  }));
}

// --- Helpers ---

function parseFrontmatter(content: string): { name: string; description: string } {
  if (!content.startsWith("---")) return { name: "", description: "" };

  const end = content.indexOf("---", 3);
  if (end < 0) return { name: "", description: "" };

  const frontmatter = content.slice(3, end);
  let name = "";
  let description = "";

  for (const line of frontmatter.split("\n")) {
    const trimmed = line.trim();
    if (trimmed.startsWith("name:")) {
      name = trimmed.slice(5).trim().replace(/^["']|["']$/g, "");
    } else if (trimmed.startsWith("description:")) {
      description = trimmed.slice(12).trim().replace(/^["']|["']$/g, "");
    }
  }

  return { name, description };
}

function getExtension(filename: string): string {
  const dot = filename.lastIndexOf(".");
  return dot >= 0 ? filename.slice(dot) : "";
}

function removeExtension(filename: string): string {
  const dot = filename.lastIndexOf(".");
  return dot >= 0 ? filename.slice(0, dot) : filename;
}

function stripFirstSegment(path: string): string {
  const slash = path.indexOf("/");
  return slash >= 0 ? path.slice(slash + 1) : "";
}

function getDirectory(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash >= 0 ? path.slice(0, slash) : "";
}

function getFirstSegment(path: string): string {
  const slash = path.indexOf("/");
  return slash >= 0 ? path.slice(0, slash) : path;
}
