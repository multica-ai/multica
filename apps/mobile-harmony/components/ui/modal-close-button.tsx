/**
 * HarmonyOS port of apps/mobile/components/ui/modal-close-button.tsx.
 * Close icon (✕) for modal sheets — matches the iOS "close-in-a-circle"
 * pattern used by Linear / Things on mobile create sheets — visually pairs
 * with the submit button on the opposite side.
 *
 * Implementation goes through `<IconButton variant="secondary">` (the
 * ported Button) so the secondary background + pressed state + dark-mode
 * color flip all come from the design-system tokens; the style override
 * locks the 28pt circular shape Linear / Things use for this slot (the
 * default icon size is a 40pt square box).
 *
 * iOS navigated via expo-router's router.back(); here the default press
 * pops the hand-rolled stack navigator (src/navigation/navigator.tsx),
 * with an `onPress` override for callers whose close target is a
 * BottomSheet rather than a stack route.
 */
import { IconButton } from "@/components/ui/icon-button";
import { useNav } from "@/src/navigation/navigator";

export function ModalCloseButton({ onPress }: { onPress?: () => void }) {
  const nav = useNav();
  return (
    <IconButton
      name="close"
      iconSize={18}
      variant="secondary"
      style={styles.round}
      onPress={onPress ?? (() => nav.pop())}
      accessibilityLabel="Close"
    />
  );
}

// size-7 rounded-full (28pt circle vs the 40pt square icon button)
const styles = {
  round: {
    width: 28,
    height: 28,
    borderRadius: 14,
    paddingHorizontal: 0,
  },
};
