import type { AuthSessionUserId } from "../shared/auth-session";

export {
  AUTH_SESSION_STATE_CHANNEL,
  parseAuthSessionUserId,
} from "../shared/auth-session";

/**
 * Keeps every window bound to one authenticated account. Tokens never cross
 * renderer boundaries: only the resolved user id is compared. Main windows
 * share the session token through storage, so the latest main-window report
 * is the account; a logout or account switch reported by any main window
 * closes every other window instead of leaving it signed in to the old one.
 */
export class AuthSessionCoordinator<T> {
  private accountUserId: AuthSessionUserId | undefined;
  private readonly mainUserIds = new Map<T, AuthSessionUserId | undefined>();
  private readonly issueUserIds = new Map<
    T,
    AuthSessionUserId | undefined
  >();

  constructor(private readonly closeWindow: (window: T) => void) {}

  registerMainWindow(window: T): void {
    this.mainUserIds.set(window, undefined);
  }

  unregisterMainWindow(window: T): void {
    this.mainUserIds.delete(window);
  }

  registerIssueWindow(window: T): void {
    this.issueUserIds.set(window, undefined);
  }

  unregisterIssueWindow(window: T): void {
    this.issueUserIds.delete(window);
  }

  hasActiveMainSession(window: T): boolean {
    const userId = this.mainUserIds.get(window);
    return typeof userId === "string" && userId === this.accountUserId;
  }

  isCurrentIssueSession(window: T): boolean {
    return (
      typeof this.accountUserId === "string" &&
      this.issueUserIds.get(window) === this.accountUserId
    );
  }

  reportMain(window: T, userId: AuthSessionUserId): boolean {
    if (!this.mainUserIds.has(window)) return false;
    const becameLoggedOut = userId === null && this.accountUserId !== null;
    const accountChanged =
      this.accountUserId !== undefined && this.accountUserId !== userId;
    this.accountUserId = userId;
    this.mainUserIds.set(window, userId);

    if (userId === null || accountChanged) {
      this.closeAllExcept(window);
      return becameLoggedOut || accountChanged;
    }

    for (const [other, otherUserId] of [...this.mainUserIds]) {
      if (other !== window && otherUserId !== undefined && otherUserId !== userId) {
        this.close(other);
      }
    }
    for (const [issueWindow, issueUserId] of [...this.issueUserIds]) {
      if (issueUserId !== undefined && issueUserId !== userId) {
        this.close(issueWindow);
      }
    }
    return false;
  }

  reportIssue(window: T, userId: AuthSessionUserId): void {
    if (!this.issueUserIds.has(window)) return;
    this.issueUserIds.set(window, userId);
    if (
      userId === null ||
      (this.accountUserId !== undefined && this.accountUserId !== userId)
    ) {
      this.close(window);
    }
  }

  private closeAllExcept(keep: T): void {
    for (const window of [...this.mainUserIds.keys(), ...this.issueUserIds.keys()]) {
      if (window !== keep) this.close(window);
    }
  }

  private close(window: T): void {
    this.mainUserIds.delete(window);
    this.issueUserIds.delete(window);
    this.closeWindow(window);
  }
}
