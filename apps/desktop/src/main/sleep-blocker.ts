import type { PreventSleepMode } from "../shared/daemon-types";

/** The slice of Electron's `powerSaveBlocker` this module drives. */
export interface IdleSleepBlocker {
  /** Starts blocking system idle sleep and returns the blocker id. */
  start: () => number;
  stop: (id: number) => void;
}

export interface SleepBlocker {
  setMode: (mode: PreventSleepMode) => void;
  setAgentsWorking: (working: boolean) => void;
}

/**
 * Holds one idle-sleep block while the mode asks for it: always in "on", only
 * while the local daemon reports claimed tasks in "agent", never in "off".
 * The display may still turn off, and closing the lid still sleeps the machine.
 */
export function createSleepBlocker(blocker: IdleSleepBlocker): SleepBlocker {
  let mode: PreventSleepMode = "off";
  let agentsWorking = false;
  let blockerId: number | null = null;

  const apply = () => {
    const wanted = mode === "on" || (mode === "agent" && agentsWorking);
    if (wanted && blockerId === null) {
      blockerId = blocker.start();
    } else if (!wanted && blockerId !== null) {
      blocker.stop(blockerId);
      blockerId = null;
    }
  };

  return {
    setMode: (next) => {
      mode = next;
      apply();
    },
    setAgentsWorking: (working) => {
      agentsWorking = working;
      apply();
    },
  };
}
