/**
 * HarmonyOS port of apps/mobile/components/ui/avatar.tsx. The iOS version
 * wraps @rn-primitives/avatar (Root + Image + Fallback with loading state);
 * the primitives are not available on RNOH, so this is a pure-RN equivalent
 * with the same three-part API:
 *
 *   <Avatar><AvatarImage source={...} /><AvatarFallback>…</AvatarFallback></Avatar>
 *
 * The fallback renders as an absolutely-positioned layer underneath the
 * image, so it shows through while the image loads; the image unmounts
 * itself on a load error (and when given an empty source), which is the
 * primitive's "show fallback" behavior.
 *
 * Visual values are the tailwind defaults: size-8 (32), rounded-full,
 * bg-muted fallback centered content.
 */
import React, { useState } from "react";
import {
  Image,
  StyleSheet,
  View,
  type ImageProps,
  type ViewProps,
} from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";

const Avatar = React.forwardRef<View, ViewProps>(({ style, ...props }, ref) => (
  <View
    ref={ref}
    style={[styles.base, style]}
    {...props}
  />
));
Avatar.displayName = "Avatar";

const AvatarImage = ({ style, source, ...props }: ImageProps) => {
  const [failed, setFailed] = useState(false);
  // An empty/absent source would throw on the native side — treat it as a
  // load failure so the fallback beneath shows instead.
  const uri =
    source && !Array.isArray(source)
      ? typeof source === "number"
        ? source
        : (source.uri ?? null)
      : null;
  if (failed || !uri) return null;
  return (
    <Image
      source={source}
      onError={() => setFailed(true)}
      style={[styles.image, style]}
      {...props}
    />
  );
};

const AvatarFallback = ({ style, ...props }: ViewProps) => {
  const c = useThemeColors();
  return (
    <View
      style={[styles.fallback, { backgroundColor: c.muted }, style]}
      {...props}
    />
  );
};

const styles = StyleSheet.create({
  base: {
    width: 32,
    height: 32,
    borderRadius: 9999,
    overflow: "hidden",
    position: "relative",
  },
  image: { width: "100%", height: "100%" },
  fallback: {
    ...StyleSheet.absoluteFillObject,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 9999,
  },
});

export { Avatar, AvatarFallback, AvatarImage };
