import { useCallback, useMemo } from "react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { selectIsPinned, usePinInputStore } from "./store";

export interface UseInputPinResult {
  /** Feature flag + caller opted in. When false, callers should hide the button entirely. */
  enabled: boolean;
  isPinned: boolean;
  togglePin: () => void;
  unpin: () => void;
}

/**
 * Returns the per-input pin state. Inputs that pass `enabled=false` (e.g.
 * channels / DMs) opt out of the affordance entirely — the hook still runs
 * to keep hook order stable, but every flag is forced to a no-op.
 */
export function useInputPin(pinKey: string, enabled: boolean): UseInputPinResult {
  const flagEnabled = useFeatureFlag("cerebro_pin_input");
  const isPinned = usePinInputStore(selectIsPinned(pinKey));
  const pin = usePinInputStore((s) => s.pin);
  const unpinAction = usePinInputStore((s) => s.unpin);

  const effective = enabled && flagEnabled;

  const togglePin = useCallback(() => {
    if (!effective) return;
    if (isPinned) {
      unpinAction(pinKey);
    } else {
      pin(pinKey);
    }
  }, [effective, isPinned, pin, unpinAction, pinKey]);

  const unpin = useCallback(() => {
    if (!effective) return;
    unpinAction(pinKey);
  }, [effective, unpinAction, pinKey]);

  return useMemo(
    () => ({
      enabled: effective,
      isPinned: effective && isPinned,
      togglePin,
      unpin,
    }),
    [effective, isPinned, togglePin, unpin],
  );
}
