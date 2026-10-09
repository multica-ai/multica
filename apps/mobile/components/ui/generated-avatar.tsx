/**
 * Mobile GeneratedAvatar — react-native-svg implementation (MAKE-291).
 *
 * Thin platform renderer: all five archetypes (Fox, Bear, Owl, Dragon, Android)
 * use approved illustrated raster catalogs via Expo Image; the generic
 * procedural scene mapped to react-native-svg remains the fallback for
 * unsupported archetypes or missing assets. Both paths derive from the same
 * persisted seed so selection remains stable across platforms and restarts.
 *
 * Gradient ids are content-addressed in the scene builder; react-native-svg
 * resolves url(#id) within its own <Svg>, so same-id gradients (always
 * identical by construction) are safe.
 */
import { Image } from "expo-image";
import Svg, {
  Circle,
  Defs,
  Ellipse,
  G,
  Line,
  LinearGradient,
  Path,
  RadialGradient,
  Rect,
  Stop,
} from "react-native-svg";
import {
  buildAvatarScene,
  deriveAvatarDesign,
  deriveIllustratedVariant,
  type AvatarGradient,
  type AvatarShape,
} from "@/lib/avatar-seed";
import { getIllustratedAvatarAsset, hasIllustratedAvatarAsset } from "@/lib/avatar-catalog";

interface GeneratedAvatarProps {
  /** The persisted avatar_seed (the payload of a `gen:<seed>` marker). */
  seed: string;
  size: number;
  /** Accessibility label — mirrors the web `role="img"` aria-label. */
  accessibilityLabel?: string;
}

function SceneGradient({ grad }: { grad: AvatarGradient }) {
  const stops = grad.stops.map((s, i) => (
    <Stop
      key={i}
      offset={String(s.offset)}
      stopColor={s.color}
      stopOpacity={s.opacity !== undefined ? String(s.opacity) : undefined}
    />
  ));
  if (grad.kind === "linear") {
    return (
      <LinearGradient id={grad.id} x1={grad.x1} y1={grad.y1} x2={grad.x2} y2={grad.y2}>
        {stops}
      </LinearGradient>
    );
  }
  return (
    <RadialGradient id={grad.id} cx={grad.cx} cy={grad.cy} r={grad.r}>
      {stops}
    </RadialGradient>
  );
}

function SceneShape({ shape }: { shape: AvatarShape }) {
  // Same rule as web: SVG's default fill is black, so stroke-only shapes
  // must explicitly opt out.
  const fill = shape.fill ?? (shape.stroke !== undefined ? "none" : undefined);
  const common = {
    fill,
    stroke: shape.stroke,
    strokeWidth: shape.strokeWidth,
    opacity: shape.opacity,
    strokeLinecap: shape.strokeLinecap,
    strokeLinejoin: shape.strokeLinejoin,
  };
  switch (shape.type) {
    case "rect":
      return (
        <Rect
          {...common}
          x={shape.x}
          y={shape.y}
          width={shape.width}
          height={shape.height}
          rx={shape.rx}
        />
      );
    case "circle":
      return <Circle {...common} cx={shape.cx} cy={shape.cy} r={shape.r} />;
    case "ellipse":
      return (
        <Ellipse {...common} cx={shape.cx} cy={shape.cy} rx={shape.rx} ry={shape.ry} />
      );
    case "line":
      return (
        <Line {...common} x1={shape.x1} y1={shape.y1} x2={shape.x2} y2={shape.y2} />
      );
    case "path":
      return <Path {...common} d={shape.d} />;
  }
}

export function GeneratedAvatar({
  seed,
  size,
  accessibilityLabel,
}: GeneratedAvatarProps) {
  const design = deriveAvatarDesign(seed);

  const variant = deriveIllustratedVariant(design.archetype, seed);
  // Illustrated assets win only when the catalog actually holds them; an
  // unsupported archetype or missing asset falls through to the generic
  // procedural renderer below.
  if (variant && hasIllustratedAvatarAsset(design.archetype, variant)) {
    // Mobile has no browser srcSet selection; request the 2x tier explicitly
    // (bounded by the catalog's 96px maximum) for Retina-density displays.
    const asset = getIllustratedAvatarAsset(
      design.archetype,
      variant,
      Math.min(size * 2, 96),
    );
    return (
      <Image
        source={{ uri: asset.src }}
        style={{ width: size, height: size }}
        contentFit="contain"
        accessibilityLabel={accessibilityLabel ?? ""}
        accessible
        cachePolicy="memory-disk"
      />
    );
  }

  const scene = buildAvatarScene(design);
  return (
    <Svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      accessibilityLabel={accessibilityLabel ?? ""}
    >
      {scene.defs.length > 0 && (
        <Defs>
          {scene.defs.map((g) => (
            <SceneGradient key={g.id} grad={g} />
          ))}
        </Defs>
      )}
      <G>
        {scene.shapes.map((shape, i) => (
          <SceneShape key={i} shape={shape} />
        ))}
      </G>
    </Svg>
  );
}
