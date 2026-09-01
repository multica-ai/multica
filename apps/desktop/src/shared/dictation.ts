/** Parameterless IPC shared by main and preload; never accepts keys or audio. */
export const CODEX_DICTATION_CHANNEL = "dictation:toggle-codex";

/** Private bridge world, distinct from the page (0) and Electron preload (999). */
export const CODEX_DICTATION_WORLD = 1004;
