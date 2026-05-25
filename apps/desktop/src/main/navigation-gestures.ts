import type { BrowserWindow } from "electron";
import {
  NAVIGATION_GESTURE_CHANNEL,
  navigationGestureFromSwipe,
} from "../shared/navigation-gestures";

export function installNavigationGestures(
  win: BrowserWindow,
  platform: NodeJS.Platform = process.platform,
): void {
  if (platform !== "darwin") return;

  win.on("swipe", (_event, direction) => {
    const gesture = navigationGestureFromSwipe(direction);
    if (!gesture) return;
    win.webContents.send(NAVIGATION_GESTURE_CHANNEL, gesture);
  });
}
