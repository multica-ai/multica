/**
 * HarmonyOS port of apps/mobile/components/ui/header.tsx — single row,
 * slot-based screen header rendered in the screen's JSX (NOT a navigation
 * header), so dynamic content reaches it as plain props instead of
 * navigation-options closures.
 *
 *   <Header title="Inbox" right={<HeaderActions />} />
 *   <Header center={<ChatTitleButton ... />} right={...} />
 *
 * Self-handles the top safe area via @/lib/safe-area (the API-compatible
 * local shim — the tpl safe-area package's JS component layer is not wired
 * on this matrix). Colors come from useThemeColors() so dark mode flips
 * automatically — the iOS version expressed the same through NativeWind
 * tokens bg-background / text-foreground / border-border.
 *
 * For pushed screens keep using the stack navigator's screen chrome; this
 * component is for tab roots only.
 */
import type { ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import { SafeAreaView } from "@/lib/safe-area";
import { Text } from "@/components/ui/text";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  /** Title text (fallback when `center` is not provided). */
  title?: string;
  /** Optional subtitle below the title. Ignored when `center` is provided. */
  subtitle?: string;
  /** Centered custom node — wins over `title`. Use for tappable titles, agent pickers, etc. */
  center?: ReactNode;
  /** Leading slot (left of title). Use sparingly — most screens just leave it null. */
  left?: ReactNode;
  /** Trailing slot — action buttons, menus, search/add toolbar. */
  right?: ReactNode;
}

export function Header({ title, subtitle, center, left, right }: Props) {
  const c = useThemeColors();
  const s = styles(c);
  return (
    <SafeAreaView edges={["top"]} style={s.safe}>
      <View style={s.row}>
        {left ? <View style={s.left}>{left}</View> : null}
        <View style={s.center}>
          {center ?? (
            title ? (
              <>
                <Text
                  variant="default"
                  style={s.title}
                  numberOfLines={1}
                >
                  {title}
                </Text>
                {subtitle ? (
                  <Text variant="muted" numberOfLines={1}>
                    {subtitle}
                  </Text>
                ) : null}
              </>
            ) : null
          )}
        </View>
        {right ? <View style={s.right}>{right}</View> : null}
      </View>
    </SafeAreaView>
  );
}

const styles = (c: ReturnType<typeof useThemeColors>) =>
  StyleSheet.create({
    safe: {
      backgroundColor: c.background,
      borderBottomWidth: 1,
      borderBottomColor: c.border,
    },
    // h-12 px-2 flex-row items-center
    row: { flexDirection: "row", alignItems: "center", height: 48, paddingHorizontal: 8 },
    left: { flexDirection: "row", alignItems: "center" },
    center: { flex: 1, paddingHorizontal: 8, justifyContent: "center" },
    right: { flexDirection: "row", alignItems: "center", gap: 4 },
    // iOS: text-lg font-semibold text-foreground
    title: { fontSize: 18, fontWeight: "600", color: c.foreground },
  });
