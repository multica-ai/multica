export function isActiveWorkspaceMember(member: { status?: string | null }): boolean {
  return (member.status ?? "active") === "active";
}
