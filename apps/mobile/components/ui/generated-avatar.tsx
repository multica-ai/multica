/**
 * Mobile GeneratedAvatar — react-native-svg implementation (MAKE-291).
 *
 * Geometry mirrors packages/ui/components/common/generated-avatar.tsx: same
 * 64×64 viewBox, same layered structure (two seeded hue fields → texture →
 * robot mark), same variant geometry. The design derives purely from the
 * persisted avatar_seed via lib/avatar-seed.ts (mobile's copy of the shared
 * algorithm), so web and mobile render the same identity for the same seed.
 *
 * The parent supplies the circular clip (borderRadius on the wrapper in
 * actor-avatar.tsx), exactly like the web renderer's rounded-full container.
 */
import Svg, { Circle, G, Line, Rect } from "react-native-svg";
import { deriveAvatarDesign } from "@/lib/avatar-seed";

interface GeneratedAvatarProps {
  /** The persisted avatar_seed (the payload of a `gen:<seed>` marker). */
  seed: string;
  size: number;
  /** Accessibility label — mirrors the web `role="img"` aria-label. */
  accessibilityLabel?: string;
}

export function GeneratedAvatar({
  seed,
  size,
  accessibilityLabel,
}: GeneratedAvatarProps) {
  const { hue, hue2, pattern, face, antenna } = deriveAvatarDesign(seed);
  const eye = `hsl(${hue}, 45%, 26%)`;
  return (
    <Svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      accessibilityLabel={accessibilityLabel ?? ""}
    >
      <Rect x="0" y="0" width="64" height="64" fill={`hsl(${hue}, 58%, 42%)`} />
      <Rect
        x="0"
        y="0"
        width="64"
        height="64"
        fill={`hsl(${hue2}, 62%, 34%)`}
        opacity="0.9"
      />

      {pattern === 1 && (
        <G stroke="#fff" strokeOpacity="0.10" strokeWidth="6" strokeLinecap="round">
          <Line x1="-10" y1="22" x2="40" y2="-8" />
          <Line x1="-4" y1="46" x2="54" y2="10" />
          <Line x1="8" y1="66" x2="66" y2="30" />
          <Line x1="30" y1="74" x2="74" y2="46" />
        </G>
      )}
      {pattern === 2 && (
        <G fill="#fff" fillOpacity="0.12">
          <Circle cx="12" cy="14" r="3" />
          <Circle cx="34" cy="9" r="3" />
          <Circle cx="55" cy="18" r="3" />
          <Circle cx="8" cy="38" r="3" />
          <Circle cx="30" cy="32" r="3" />
          <Circle cx="54" cy="42" r="3" />
          <Circle cx="16" cy="58" r="3" />
          <Circle cx="44" cy="56" r="3" />
        </G>
      )}
      {pattern === 3 && (
        <G fill="none" stroke="#fff" strokeOpacity="0.12" strokeWidth="4">
          <Circle cx="32" cy="32" r="12" />
          <Circle cx="32" cy="32" r="24" />
          <Circle cx="32" cy="32" r="36" />
        </G>
      )}

      {antenna && (
        <Line
          x1="32"
          y1="16"
          x2="32"
          y2="8"
          stroke="#fff"
          strokeOpacity="0.95"
          strokeWidth="3"
          strokeLinecap="round"
        />
      )}
      {antenna ? (
        <Circle cx="32" cy="7" r="3.5" fill="#fff" fillOpacity="0.95" />
      ) : (
        <G fill="#fff" fillOpacity="0.95">
          <Rect x="9" y="27" width="7" height="14" rx="3.5" />
          <Rect x="48" y="27" width="7" height="14" rx="3.5" />
        </G>
      )}
      <Rect
        x="15"
        y="16"
        width="34"
        height="34"
        rx="11"
        fill="#fff"
        fillOpacity="0.95"
      />
      {face === 0 && (
        <Rect x="21" y="27" width="22" height="9" rx="4.5" fill={eye} />
      )}
      {face === 1 && (
        <G fill={eye}>
          <Circle cx="25.5" cy="31.5" r="4.5" />
          <Circle cx="38.5" cy="31.5" r="4.5" />
        </G>
      )}
      {face === 2 && (
        <G fill={eye}>
          <Rect x="21" y="27" width="8.5" height="9" rx="2" />
          <Rect x="34.5" y="27" width="8.5" height="9" rx="2" />
        </G>
      )}
      <Rect
        x="26"
        y="41"
        width="12"
        height="3.5"
        rx="1.75"
        fill={eye}
        fillOpacity="0.75"
      />
    </Svg>
  );
}
