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

  for (let i = 0; i < files.length; i++) {
    const file = files[i]!;
    const ext = getExtension(file.name);
    if (!textExtensions.has(ext)) continue;
    if (file.size > 1024 * 1024) continue; // skip files > 1MB

    promises.push(
      file.text().then((content) => {
        // webkitRelativePath is like "dirname/subdir/file.md"
        const relativePath = file.webkitRelativePath;
        // Strip the top-level directory name
        const stripped = stripFirstSegment(relativePath);
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
  const codexFile = files.find(
    (f) => f.relativePath === "AGENTS.md" || f.relativePath === "codex.md",
  );
  if (!codexFile) return null;

  const { name, description } = parseFrontmatter(codexFile.content);
  const supportingFiles = files.filter(
    (f) => f !== codexFile && f.relativePath !== "AGENTS.md" && f.relativePath !== "codex.md",
  );

  return {
    name: name || "AGENTS",
    description: description || "Imported from Codex format",
    content: codexFile.content,
    files: supportingFiles.map((f) => ({ path: f.relativePath, content: f.content })),
  };
}

function parseSkillMdFormat(files: ParsedFile[]): CreateSkillRequest[] {
  // Find all directories that contain a SKILL.md
  const skillDirs = new Set<string>();
  for (const f of files) {
    const dir = getDirectory(f.relativePath);
    if (f.relativePath === `${dir}/SKILL.md` && dir !== "") {
      skillDirs.add(dir);
    }
  }

  if (skillDirs.size === 0) return [];

  const skills: CreateSkillRequest[] = [];

  for (const dir of skillDirs) {
    // Collect ALL files under this directory, including subdirectories
    const skillMd = files.find((f) => f.relativePath === `${dir}/SKILL.md`);
    if (!skillMd) continue;

    const { name, description } = parseFrontmatter(skillMd.content);
    const prefix = dir + "/";
    const supportingFiles = files.filter(
      (f) => f.relativePath.startsWith(prefix) && f.relativePath !== `${dir}/SKILL.md`,
    );
    const dirName = dir.includes("/") ? dir.split("/").pop()! : dir;

    skills.push({
      name: name || dirName,
      description: description || "",
      content: skillMd.content,
      files: supportingFiles.map((f) => ({
        path: f.relativePath.slice(prefix.length),
        content: f.content,
      })),
    });
  }

  return skills;
}

function parseFallbackFormat(files: ParsedFile[]): CreateSkillRequest[] {
  const rootMdFiles = files.filter(
    (f) => !f.relativePath.includes("/") && f.relativePath.endsWith(".md"),
  );

  if (rootMdFiles.length === 0) {
    // No root .md files — package everything as a single skill
    return [{
      name: "Imported Skills",
      description: "",
      content: files.find((f) => f.relativePath.endsWith(".md"))?.content || "",
      files: files
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
