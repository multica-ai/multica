/**
 * Close icon (✕) rendered in the modal Stack header. Matches the iOS
 * "close-in-a-circle" pattern used by Linear / Things on mobile create
 * sheets — visually pairs with the submit button on the opposite side.
 *
 * Implementation goes through `<IconButton variant="secondary">` (RNR
 * Button under the hood) so the secondary background + active state +
 * dark-mode color flip all come from the design-system tokens. The
 * className override locks the 28pt circular shape Linear / Things use
 * for this slot (RNR's default `size="icon"` is a 40pt square box).
 *
 * Used by `[workspace]/_layout.tsx` for the new-issue, search, and
 * new-comment modals.
 */
import { router } from "expo-router";
import { IconButton } from "@/components/ui/icon-button";

export function ModalCloseButton() {
  return (
    <IconButton
      name="close"
      iconSize={18}
      variant="secondary"
      className="size-7 rounded-full"
      onPress={() => router.back()}
      accessibilityLabel="Close"
    />
  );
}
