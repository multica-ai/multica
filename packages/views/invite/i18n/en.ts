import type { InviteDict } from "./types";

export function createEnDict(): InviteDict {
  return {
    shell: {
      back: "Back",
      logOut: "Log out",
    },
    notFound: {
      title: "Invitation not found",
      description:
        "This invitation may have expired, been revoked, or doesn't belong to your account.",
      goToDashboard: "Go to dashboard",
    },
    accepted: {
      titlePrefix: "You joined ",
      titleSuffix: "!",
      redirecting: "Redirecting to workspace...",
    },
    declined: {
      title: "Invitation declined",
      description: "You won't be added to this workspace.",
      goToDashboard: "Go to dashboard",
    },
    invitation: {
      joinTitlePrefix: "Join ",
      workspaceFallback: "workspace",
      invitedAsAdminPrefix: "",
      invitedAsAdminSuffix: " invited you to join as an admin.",
      invitedAsMemberPrefix: "",
      invitedAsMemberSuffix: " invited you to join as a member.",
      alreadyHandledAccepted: "This invitation has already been accepted.",
      alreadyHandledDeclined: "This invitation has already been declined.",
      expired: "This invitation has expired.",
      accept: "Accept & Join",
      accepting: "Joining...",
      decline: "Decline",
      declining: "Declining...",
      acceptFailed: "Failed to accept invitation",
      declineFailed: "Failed to decline invitation",
    },
  };
}
