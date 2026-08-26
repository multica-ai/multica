import { api } from "@multica/core/api";

/**
 * Downloads a skill as a portable .tar.gz archive and triggers a browser
 * download using the server-provided filename. Shared by the skills-list row
 * kebab and the skill detail page so both affordances behave identically.
 *
 * The blob path (not a URL navigation) is required because the export endpoint
 * is auth-gated: the browser would otherwise save the 401 body as the file.
 */
export async function downloadSkillArchive(
  skillId: string,
): Promise<string> {
  const { blob, filename } = await api.exportSkillArchive(skillId);
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.style.display = "none";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
  return filename;
}
