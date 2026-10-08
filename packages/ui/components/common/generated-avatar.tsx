"use client";

import { cn } from "@multica/ui/lib/utils";
import {
  buildAvatarScene,
  deriveAvatarDesign,
  deriveIllustratedVariant,
  type AvatarGradient,
  type AvatarShape,
} from "@multica/ui/lib/avatar-seed";
import { getIllustratedAvatarAsset, hasIllustratedAvatarAsset } from "@multica/ui/lib/avatar-catalog";

interface GeneratedAvatarProps {
  /** The persisted avatar_seed (the payload of a `gen:<seed>` marker). */
  seed: string;
  /** Accessible label / img alt equivalent. */
  name: string;
  /** Rendered at whatever size the avatar circle is; scales via viewBox. */
  className?: string;
}

/**
 * Deterministic generated agent avatar (MAKE-291). An original chibi
 * companion character — one of five archetypes (fox, bear, owl, dragon,
 * android) with an authored silhouette, gradient-shaded fur/metal, a
 * coordinated outfit, reference-style expressive eyes, and a seed-stable
 * role accessory. All five archetypes (Fox, Bear, Owl, Dragon, Android) use approved
 * illustrated raster assets; the generic procedural SVG renderer remains as the
 * fallback for unsupported archetypes or missing assets. Design derives
 * purely from `seed` (see `lib/avatar-seed.ts`) so identity is stable across
 * renames, refreshes, and restarts, with matching web/desktop/mobile selection
 * semantics.
 */
function GeneratedAvatar({ seed, name, className }: GeneratedAvatarProps) {
  const design = deriveAvatarDesign(seed);

  const variant = deriveIllustratedVariant(design.archetype, seed);
  // Illustrated assets win only when the catalog actually holds them; an
  // unsupported archetype or missing asset falls through to the generic
  // procedural renderer below.
  if (variant && hasIllustratedAvatarAsset(design.archetype, variant)) {
    const asset = getIllustratedAvatarAsset(design.archetype, variant, 48);
    return (
      <img
        src={asset.src}
        srcSet={asset.srcSet}
        sizes="(max-width: 32px) 32px, (max-width: 48px) 48px, 96px"
        alt={name}
        className={cn("h-full w-full object-contain", className)}
        data-slot="generated-avatar"
        data-avatar-seed={seed}
        data-avatar-archetype={design.archetype}
        data-avatar-variant={variant}
        draggable={false}
        decoding="async"
      />
    );
  }

  const scene = buildAvatarScene(design);
  return (
    <svg
      viewBox="0 0 64 64"
      role="img"
      aria-label={name}
      className={cn("h-full w-full", className)}
      data-slot="generated-avatar"
      data-avatar-seed={seed}
      data-avatar-archetype={design.archetype}
    >
      <GeneratedAvatarArt defs={scene.defs} shapes={scene.shapes} />
    </svg>
  );
}

/** Translates one scene gradient to a DOM <defs> child. */
function SceneGradient({ grad }: { grad: AvatarGradient }) {
  if (grad.kind === "linear") {
    return (
      <linearGradient id={grad.id} x1={grad.x1} y1={grad.y1} x2={grad.x2} y2={grad.y2}>
        {grad.stops.map((s, i) => (
          <stop key={i} offset={s.offset} stopColor={s.color} stopOpacity={s.opacity} />
        ))}
      </linearGradient>
    );
  }
  return (
    <radialGradient id={grad.id} cx={grad.cx} cy={grad.cy} r={grad.r}>
      {grad.stops.map((s, i) => (
        <stop key={i} offset={s.offset} stopColor={s.color} stopOpacity={s.opacity} />
      ))}
    </radialGradient>
  );
}

/**
 * The paint itself, split out so tests can assert on the scene-driven
 * structure without mounting the wrapper. Geometry is fixed in a 64×64
 * space; the circle clip lives in the parent avatar container
 * (rounded-full), so corners stay square here on purpose.
 */
function GeneratedAvatarArt({
  defs,
  shapes,
}: {
  defs: AvatarGradient[];
  shapes: AvatarShape[];
}) {
  return (
    <>
      {defs.length > 0 && (
        <defs>
          {defs.map((g) => (
            <SceneGradient key={g.id} grad={g} />
          ))}
        </defs>
      )}
      {shapes.map((shape, i) => {
        const common = {
          key: i,
          // SVG's default fill is black — a stroke-only shape (rings, arcs)
          // must explicitly opt out or it paints a black blob.
          fill: shape.fill ?? (shape.stroke !== undefined ? "none" : undefined),
          stroke: shape.stroke,
          strokeWidth: shape.strokeWidth,
          opacity: shape.opacity,
          strokeLinecap: shape.strokeLinecap,
          strokeLinejoin: shape.strokeLinejoin,
        };
        switch (shape.type) {
          case "rect":
            return (
              <rect
                {...common}
                x={shape.x}
                y={shape.y}
                width={shape.width}
                height={shape.height}
                rx={shape.rx}
              />
            );
          case "circle":
            return <circle {...common} cx={shape.cx} cy={shape.cy} r={shape.r} />;
          case "ellipse":
            return (
              <ellipse {...common} cx={shape.cx} cy={shape.cy} rx={shape.rx} ry={shape.ry} />
            );
          case "line":
            return (
              <line {...common} x1={shape.x1} y1={shape.y1} x2={shape.x2} y2={shape.y2} />
            );
          case "path":
            return <path {...common} d={shape.d} />;
        }
      })}
    </>
  );
}

export { GeneratedAvatar, type GeneratedAvatarProps };
