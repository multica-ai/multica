import type { MemberRole } from "../types";

/**
 * Owners and admins change the company state, the quiet hours and what the
 * governance log shows. Mirrors the server's role gate; the server still
 * refuses a write that a member sends anyway.
 */
export function canGovern(role: MemberRole | null | undefined): boolean {
  return role === "owner" || role === "admin";
}
