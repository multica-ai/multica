import type { ComponentType } from "react";

/**
 * Secret scanning is a commercial feature (packages/ee). The open-source views
 * only leave room for it: a workspace settings page and a notice under a
 * comment's text. Without an extension neither appears.
 */
export interface SecretScanningExtension {
  /** The workspace settings page, managed by owners and admins. */
  settings: {
    value: string;
    icon: ComponentType<{ className?: string }>;
    useLabel: () => string;
    Content: ComponentType;
  };
  /** Under a comment's text: what was found in it, for the people who can act on it. */
  CommentNotice: ComponentType<{ issueId: string; commentId: string }>;
  /** Mounted once per workspace layout, for realtime sync. */
  Host: ComponentType;
}

let extension: SecretScanningExtension | null = null;

/** Called once at app start, before the first render. */
export function registerSecretScanningExtension(next: SecretScanningExtension | null): void {
  extension = next;
}

export function getSecretScanningExtension(): SecretScanningExtension | null {
  return extension;
}

function useNoLabel(): string {
  return "";
}

/** The settings page's label, or "" when no extension provides the page. */
export function useSecretScanningSettingsLabel(): string {
  // Registration happens before the first render and never changes, so
  // every render calls the same hook.
  const useLabel = extension?.settings.useLabel ?? useNoLabel;
  return useLabel();
}
