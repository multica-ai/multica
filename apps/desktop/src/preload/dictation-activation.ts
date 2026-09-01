import { contextBridge } from "electron";
import { CODEX_DICTATION_WORLD } from "../shared/dictation";

/** Runs in the isolated preload, never the page's mutable JavaScript world. */
export function installCodexDictationActivation(): void {
  let activation: { button: HTMLButtonElement; at: number } | undefined;
  let keyPress: { button: HTMLButtonElement; key: string } | undefined;

  const micButton = (event: Event): HTMLButtonElement | undefined => {
    if (!event.isTrusted || !(event.target instanceof Element)) return undefined;
    const button = event.target.closest("button[data-native-dictation]");
    if (button instanceof HTMLButtonElement && button.isConnected && !button.disabled) return button;
    return undefined;
  };
  const arm = (button: HTMLButtonElement | undefined) => {
    activation = button ? { button, at: performance.now() } : undefined;
  };
  document.addEventListener("click", (event) => arm(micButton(event)), true);
  document.addEventListener("keydown", (event) => {
    keyPress = undefined;
    if (event.repeat || event.ctrlKey || event.altKey || event.shiftKey || event.metaKey) return;
    if (event.key !== "Enter" && event.key !== " ") return;
    const button = micButton(event);
    if (button) keyPress = { button, key: event.key };
  }, true);
  document.addEventListener("keyup", (event) => {
    const pressed = keyPress;
    keyPress = undefined;
    if (pressed && pressed.key === event.key && pressed.button === micButton(event)) arm(pressed.button);
  }, true);
  window.addEventListener("blur", () => { activation = undefined; keyPress = undefined; });

  // No equivalent API is exposed in the main world. Even the generic renderer
  // IPC bridge cannot mint a grant. The marker is UI routing, not protection
  // against a compromised renderer deceiving the user with a fake mic button.
  contextBridge.exposeInIsolatedWorld(CODEX_DICTATION_WORLD, "multicaDictationActivation", {
    consume: (): boolean => {
      const grant = activation;
      activation = undefined;
      return !!grant && grant.button.isConnected && performance.now() - grant.at < 2000 &&
        document.hasFocus() && document.activeElement instanceof HTMLElement &&
        document.activeElement.isContentEditable === true;
    },
  });
}
