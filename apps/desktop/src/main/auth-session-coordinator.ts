import type { AuthSessionUserId } from "../shared/auth-session";

export {
  AUTH_SESSION_STATE_CHANNEL,
  parseAuthSessionUserId,
} from "../shared/auth-session";

/**
 * Keeps dedicated issue windows bound to the main window's authenticated
 * account. Tokens never cross renderer boundaries: only the resolved user id
 * is compared, and stale issue windows are closed on logout/account switch.
 */
export class AuthSessionCoordinator<T> {
  private mainUserId: AuthSessionUserId | undefined;
  private readonly issueUserIds = new Map<
    T,
    AuthSessionUserId | undefined
  >();

  constructor(private readonly closeIssueWindow: (window: T) => void) {}

  registerIssueWindow(window: T): void {
    this.issueUserIds.set(window, undefined);
  }

  unregisterIssueWindow(window: T): void {
    this.issueUserIds.delete(window);
  }

  hasActiveMainSession(): boolean {
    return typeof this.mainUserId === "string";
  }

  isCurrentIssueSession(window: T): boolean {
    return (
      typeof this.mainUserId === "string" &&
      this.issueUserIds.get(window) === this.mainUserId
    );
  }

  reportMain(userId: AuthSessionUserId): boolean {
    const becameLoggedOut = userId === null && this.mainUserId !== null;
    const accountChanged =
      this.mainUserId !== undefined && this.mainUserId !== userId;
    this.mainUserId = userId;

    if (userId === null || accountChanged) {
      this.closeAllIssueWindows();
      return becameLoggedOut || accountChanged;
    }

    for (const [window, issueUserId] of this.issueUserIds) {
      if (issueUserId !== undefined && issueUserId !== userId) {
        this.closeIssue(window);
      }
    }
    return false;
  }

  reportIssue(window: T, userId: AuthSessionUserId): void {
    if (!this.issueUserIds.has(window)) return;
    this.issueUserIds.set(window, userId);
    if (
      userId === null ||
      (this.mainUserId !== undefined && this.mainUserId !== userId)
    ) {
      this.closeIssue(window);
    }
  }

  private closeAllIssueWindows(): void {
    for (const window of [...this.issueUserIds.keys()]) {
      this.closeIssue(window);
    }
  }

  private closeIssue(window: T): void {
    this.issueUserIds.delete(window);
    this.closeIssueWindow(window);
  }
}
