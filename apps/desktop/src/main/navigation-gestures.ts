import type { BrowserWindow } from "electron";
import {
  isNavigationGesture,
  NAVIGATION_GESTURE_CHANNEL,
  navigationGestureFromSwipe,
  type NavigationGesture,
} from "../shared/navigation-gestures";

export function installNavigationGestures(
  win: BrowserWindow,
  platform: NodeJS.Platform = process.platform,
): void {
  const send = (gesture: NavigationGesture) => {
    win.webContents.send(NAVIGATION_GESTURE_CHANNEL, gesture);
  };

  win.webContents.on("before-mouse-event", (event, mouse) => {
    if (mouse.type !== "mouseDown" && mouse.type !== "mouseUp") return;
    // Electron's converter emits back/forward for extended mouse buttons.
    const button = mouse.button;
    if (!isNavigationGesture(button)) return;
    // Consume the click before Chromium can traverse its own history.
    event.preventDefault();
    if (mouse.type === "mouseUp") send(button);
  });

  if (platform === "darwin") {
    win.on("swipe", (_event, direction) => {
      const gesture = navigationGestureFromSwipe(direction);
      if (gesture) send(gesture);
    });
  }

  if (platform === "win32") {
    // Windows also delivers app commands outside the web contents, such as
    // over the title bar. Chromium consumes XBUTTON releases in web contents,
    // so those releases do not also generate WM_APPCOMMAND.
    win.on("app-command", (_event, command) => {
      if (command === "browser-backward") send("back");
      else if (command === "browser-forward") send("forward");
    });
  }
}
