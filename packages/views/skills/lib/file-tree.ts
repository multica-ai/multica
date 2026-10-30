export const SKILL_MAIN_FILE = "SKILL.md";

export interface SkillFileNode {
  name: string;
  path: string;
  isDirectory: boolean;
  children: SkillFileNode[];
}

/**
 * Nests a skill's file paths into folders. SKILL.md comes first, then
 * folders, then files, each by name.
 */
export function buildSkillFileTree(filePaths: readonly string[]): SkillFileNode[] {
  const root: SkillFileNode[] = [];
  for (const filePath of filePaths) {
    const parts = filePath.split("/");
    let current = root;
    for (let i = 0; i < parts.length; i++) {
      const name = parts[i]!;
      const isLast = i === parts.length - 1;
      const path = parts.slice(0, i + 1).join("/");
      let existing = current.find((n) => n.name === name);
      if (!existing) {
        existing = { name, path, isDirectory: !isLast, children: [] };
        current.push(existing);
      }
      if (!isLast) current = existing.children;
    }
  }

  const sortNodes = (nodes: SkillFileNode[]): SkillFileNode[] => {
    nodes.sort((a, b) => {
      if (a.path === SKILL_MAIN_FILE) return -1;
      if (b.path === SKILL_MAIN_FILE) return 1;
      if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    for (const node of nodes) if (node.isDirectory) sortNodes(node.children);
    return nodes;
  };
  return sortNodes(root);
}
