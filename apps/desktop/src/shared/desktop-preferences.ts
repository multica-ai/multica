export const CLOSE_BEHAVIORS = ["tray", "taskbar", "quit"] as const;

export type CloseBehavior = (typeof CLOSE_BEHAVIORS)[number];

export interface DesktopPreferences {
  closeBehavior: CloseBehavior;
}

export function isCloseBehavior(value: unknown): value is CloseBehavior {
  return CLOSE_BEHAVIORS.includes(value as CloseBehavior);
}
