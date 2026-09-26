import type { CloseBehavior } from "../shared/desktop-preferences";

interface CloseEvent {
  preventDefault(): void;
}

interface CloseableWindow {
  hide(): void;
  minimize(): void;
}

export function applyMainWindowCloseBehavior(options: {
  event: CloseEvent;
  window: CloseableWindow;
  closeBehavior: CloseBehavior;
  isQuitting: boolean;
  requestQuit: () => void;
}): void {
  if (options.isQuitting) return;

  options.event.preventDefault();
  switch (options.closeBehavior) {
    case "tray":
      options.window.hide();
      return;
    case "taskbar":
      options.window.minimize();
      return;
    case "quit":
      options.requestQuit();
  }
}
