// IPC channel main → renderer carries window-level keyboard shortcuts that
// the main process must own (it intercepts them in `before-input-event` to
// stop the application-menu accelerator from firing) but whose effect lives
// in the renderer's tab store.
export const CLOSE_ACTIVE_TAB_CHANNEL = "shortcut:close-active-tab";
