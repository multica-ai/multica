export function canManageProjects(role: string | null | undefined): boolean {
  return role === "owner" || role === "admin";
}
