"use client";

import { cn } from "@multica/ui/lib/utils";
import {
  deriveAvatarDesign,
  type GeneratedAvatarDesign,
} from "@multica/ui/lib/avatar-seed";

interface GeneratedAvatarProps {
  /** The persisted avatar_seed (the payload of a `gen:<seed>` marker). */
  seed: string;
  /** Accessible label / img alt equivalent. */
  name: string;
  /** Rendered at whatever size the avatar circle is; scales via viewBox. */
  className?: string;
}

/**
 * Deterministic generated agent avatar (MAKE-291). A geometric robot mark on
 * a seeded two-hue gradient, drawn as inline SVG so it stays crisp at every
 * avatar size and needs no network round-trip. Design derives purely from
 * `seed` — see `lib/avatar-seed.ts`; rendering only ever draws what the
 * design says, so identity is stable across renames, refreshes, and restarts.
 *
 * Deliberately restrained: gradient + one texture + one face. Anything more
 * stops reading at 16px.
 */
function GeneratedAvatar({ seed, name, className }: GeneratedAvatarProps) {
  const design = deriveAvatarDesign(seed);
  return (
    <svg
      viewBox="0 0 64 64"
      role="img"
      aria-label={name}
      className={cn("h-full w-full", className)}
      data-slot="generated-avatar"
      data-avatar-seed={seed}
    >
      <GeneratedAvatarArt design={design} />
    </svg>
  );
}

/**
 * The paint itself, split out so tests (and any future raster path) can
 * assert on the design-driven structure without mounting the wrapper.
 * Geometry is fixed in a 64×64 space; the circle clip lives in the parent
 * avatar container (rounded-full), so corners stay square here on purpose.
 */
function GeneratedAvatarArt({ design }: { design: GeneratedAvatarDesign }) {
  const { hue, hue2, pattern, face, antenna } = design;
  return (
    <>
      <rect width="64" height="64" fill={`hsl(${hue}, 58%, 42%)`} />
      <rect
        width="64"
        height="64"
        fill={`hsl(${hue2}, 62%, 34%)`}
        opacity="0.9"
      />

      {pattern === 1 && (
        <g
          stroke="#fff"
          strokeOpacity="0.10"
          strokeWidth="6"
          strokeLinecap="round"
        >
          <line x1="-10" y1="22" x2="40" y2="-8" />
          <line x1="-4" y1="46" x2="54" y2="10" />
          <line x1="8" y1="66" x2="66" y2="30" />
          <line x1="30" y1="74" x2="74" y2="46" />
        </g>
      )}
      {pattern === 2 && (
        <g fill="#fff" fillOpacity="0.12">
          <circle cx="12" cy="14" r="3" />
          <circle cx="34" cy="9" r="3" />
          <circle cx="55" cy="18" r="3" />
          <circle cx="8" cy="38" r="3" />
          <circle cx="30" cy="32" r="3" />
          <circle cx="54" cy="42" r="3" />
          <circle cx="16" cy="58" r="3" />
          <circle cx="44" cy="56" r="3" />
        </g>
      )}
      {pattern === 3 && (
        <g
          fill="none"
          stroke="#fff"
          strokeOpacity="0.12"
          strokeWidth="4"
        >
          <circle cx="32" cy="32" r="12" />
          <circle cx="32" cy="32" r="24" />
          <circle cx="32" cy="32" r="36" />
        </g>
      )}

      {/* Robot mark: neutral so it reads on any hue, chunky enough to
          survive 16px. Head + one of three face variants + antenna/ears. */}
      <g>
        {antenna && (
          <line
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
          <circle cx="32" cy="7" r="3.5" fill="#fff" fillOpacity="0.95" />
        ) : (
          <g fill="#fff" fillOpacity="0.95">
            <rect x="9" y="27" width="7" height="14" rx="3.5" />
            <rect x="48" y="27" width="7" height="14" rx="3.5" />
          </g>
        )}
        <rect
          x="15"
          y="16"
          width="34"
          height="34"
          rx="11"
          fill="#fff"
          fillOpacity="0.95"
        />
        {face === 0 && (
          <rect
            x="21"
            y="27"
            width="22"
            height="9"
            rx="4.5"
            fill={`hsl(${hue}, 45%, 26%)`}
          />
        )}
        {face === 1 && (
          <g fill={`hsl(${hue}, 45%, 26%)`}>
            <circle cx="25.5" cy="31.5" r="4.5" />
            <circle cx="38.5" cy="31.5" r="4.5" />
          </g>
        )}
        {face === 2 && (
          <g fill={`hsl(${hue}, 45%, 26%)`}>
            <rect x="21" y="27" width="8.5" height="9" rx="2" />
            <rect x="34.5" y="27" width="8.5" height="9" rx="2" />
          </g>
        )}
        {/* Mouth slot: constant across variants — one fixed landmark keeps
            the three face styles reading as the same family. */}
        <rect
          x="26"
          y="41"
          width="12"
          height="3.5"
          rx="1.75"
          fill={`hsl(${hue}, 45%, 26%)`}
          fillOpacity="0.75"
        />
      </g>
    </>
  );
}

export { GeneratedAvatar, type GeneratedAvatarProps };
