/**
 * Keyboard height for the HarmonyOS client — a deterministic replacement
 * for KeyboardAvoidingView, whose "padding" behavior on this RNOH matrix
 * overshoots (content pushed off-screen) and never resets after the
 * keyboard hides. Returns the keyboard height in layout units, 0 when
 * hidden, clamped to half the window height as a guard against
 * px/vp unit surprises in the native event.
 */
import { useEffect, useState } from "react";
import { Dimensions, Keyboard, type KeyboardEvent } from "react-native";

export function useKeyboardHeight(): number {
  const [height, setHeight] = useState(0);

  useEffect(() => {
    const onShow = (event: KeyboardEvent) => {
      const maxHeight = Dimensions.get("window").height * 0.6;
      setHeight(Math.min(event.endCoordinates?.height ?? 0, maxHeight));
    };
    const onHide = () => setHeight(0);
    const showSub = Keyboard.addListener("keyboardDidShow", onShow);
    const hideSub = Keyboard.addListener("keyboardDidHide", onHide);
    return () => {
      showSub.remove();
      hideSub.remove();
    };
  }, []);

  return height;
}
