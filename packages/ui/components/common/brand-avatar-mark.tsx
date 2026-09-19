import { useState } from "react";
import {
  AVATAR_BRAND_BY_ID,
  type AvatarBrandId,
  type AvatarBrandTier,
} from "@multica/ui/lib/avatar-brand";
import { avatarBrandAssetUrl } from "./brand-avatar-assets";

interface BrandAvatarMarkProps {
  id: AvatarBrandId;
  tier?: AvatarBrandTier | null;
  className?: string;
  label?: string;
}

/**
 * Circular LLM brand badge. The bundled artwork is already a circular badge
 * with the capability frame baked in, so this is a plain `<img>` on a round
 * clip. If the artwork ever fails to load (partial install, exotic CSP), the
 * letter tile takes over — same face a caller without asset support sees.
 */
export function BrandAvatarMark({
  id,
  tier = null,
  className,
  label,
}: BrandAvatarMarkProps) {
  const brand = AVATAR_BRAND_BY_ID[id];
  const [failed, setFailed] = useState(false);
  const src = failed ? null : avatarBrandAssetUrl(id, tier);

  if (!src) {
    return (
      <span
        role={label ? "img" : undefined}
        aria-label={label}
        aria-hidden={label ? undefined : true}
        className={
          className ??
          "flex items-center justify-center rounded-full bg-muted text-muted-foreground"
        }
        style={
          className
            ? undefined
            : { fontSize: "0.45em", fontWeight: 600, lineHeight: 1 }
        }
      >
        {brand.letter}
      </span>
    );
  }

  return (
    <img
      src={src}
      alt={label ?? ""}
      aria-hidden={label ? undefined : true}
      role={label ? "img" : undefined}
      draggable={false}
      onError={() => setFailed(true)}
      className={
        className ??
        "rounded-full object-cover select-none [user-select:none]"
      }
    />
  );
}
