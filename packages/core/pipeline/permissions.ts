import type { MemberRole } from "../types";
import type { InitiativeStatus, PipelineExitMode } from "./types";

/**
 * Owners and admins may waive artifacts, decide stage gates and force a move.
 * Mirrors the server's role gate; the server still refuses a write that a
 * member sends anyway.
 */
export function canManageInitiatives(role: MemberRole | null | undefined): boolean {
  return role === "owner" || role === "admin";
}

export type InitiativeAction = "advance" | "attest" | "send_back" | "shelve" | "resume" | "close";

export interface InitiativeActionContext {
  /** An owner or admin of the workspace. */
  admin: boolean;
  /** A human member of the squad that owns the current stage. */
  squadMember: boolean;
  /** The current stage's exit mode; null when the stage is not in the pipeline. */
  exitMode: PipelineExitMode | null;
  status: InitiativeStatus;
  /** Whether the current stage has an earlier stage to return to. */
  hasPreviousStage: boolean;
}

/**
 * Which transitions the panel offers, mirroring the server's rules in
 * handler/initiative.go and service/initiative.go:
 *
 * - Advance and Attest: the owning squad or an admin. Advance is withheld on a
 *   gate stage (the gate proposal decides) and on an exit mode this client does
 *   not know. Attest is only for attest-mode stages.
 * - Send back: the owning leader or an admin. The leader is an agent, and the
 *   server only matches a leader that is an agent, so for a human member this is
 *   an admin-only action.
 * - Shelve, Close: admin, on a live initiative (the service refuses a shelved or
 *   closed one). Resume: admin, on a shelved initiative only.
 */
export function initiativeActions(context: InitiativeActionContext): Record<InitiativeAction, boolean> {
  const live = context.status === "active" || context.status === "operating";
  const mayAct = context.admin || context.squadMember;
  return {
    advance: live && mayAct && (context.exitMode === "auto" || context.exitMode === "attest"),
    attest: live && mayAct && context.exitMode === "attest",
    send_back: live && context.admin && context.hasPreviousStage,
    shelve: live && context.admin,
    resume: context.status === "shelved" && context.admin,
    close: live && context.admin,
  };
}
