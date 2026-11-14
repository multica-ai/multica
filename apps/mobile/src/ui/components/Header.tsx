import { createContext, useContext, useLayoutEffect, useRef } from "react";
import { Platform, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { NavigationContext } from "@react-navigation/native";
import type { NativeStackHeaderItem } from "@react-navigation/native-stack";
import type { IconName } from "../icons/Icon";
import { sfSymbol } from "../icons/sf-symbols";
import { useTheme } from "../theme";
import { Capsule, FloatingButton } from "./Floating";
import { Txt } from "./Text";

export interface HeaderAction {
  icon: IconName;
  label: string;
  onPress: () => void;
  /** Draws the filled symbol, e.g. a set star. */
  filled?: boolean;
}

/**
 * Where a screen's header goes.
 * - `native`: the navigation bar of the screen's navigator, which the system lays out (on iPhone
 *   Duo it moves onto the vertical bar).
 * - `inline`: drawn at the top of the content. The browser preview, and the list column beside a
 *   detail, whose controls stay with the list.
 * - `detail`: the detail column beside a list. Its actions take the navigation bar; it has no
 *   title or back button, since the list is next to it.
 */
export type HeaderPlacement = "native" | "inline" | "detail";

export const HeaderPlacementContext = createContext<HeaderPlacement>(Platform.OS === "ios" ? "native" : "inline");

interface NativeHeader {
  title: string;
  large: boolean;
  actions: readonly HeaderAction[];
}

function nativeItems(actions: readonly HeaderAction[], latest: { current: readonly HeaderAction[] }): NativeStackHeaderItem[] {
  return actions.map((action, index) => ({
    type: "button",
    label: action.label,
    accessibilityLabel: action.label,
    icon: { type: "sfSymbol", name: sfSymbol(action.icon, action.filled) as never },
    onPress: () => latest.current[index]?.onPress(),
  }));
}

/** Writes the header into the navigator's options; only when the title or the set of actions changes. */
function useNativeHeader(header: NativeHeader | null) {
  const navigation = useContext(NavigationContext);
  const palette = useTheme();
  const latest = useRef<readonly HeaderAction[]>([]);
  const actions = header?.actions;
  useLayoutEffect(() => {
    latest.current = actions ?? [];
  });
  const actionsKey = header?.actions.map((action) => `${action.icon}:${action.label}:${action.filled ? 1 : 0}`).join("|") ?? "";
  const title = header?.title ?? "";
  const large = header?.large ?? false;
  const active = header !== null;

  useLayoutEffect(() => {
    if (!active || !navigation) return;
    const current = latest.current;
    navigation.setOptions({
      headerShown: true,
      headerTransparent: true,
      headerShadowVisible: false,
      headerTintColor: palette.label,
      // Not `title`: native tabs label the tab with it.
      headerTitle: title,
      // Native stack and native tabs name the large title differently.
      headerLargeTitle: large,
      headerLargeTitleEnabled: large,
      headerLargeTitleShadowVisible: false,
      headerBackButtonDisplayMode: "minimal",
      unstable_headerRightItems: () => nativeItems(current, latest),
    });
  }, [active, navigation, title, large, actionsKey, palette.label]);
}

interface TitleHeaderProps {
  title: string;
  actions?: readonly HeaderAction[];
}

/** Top-level screen header: the title, and actions for what the screen lists. */
export function TitleHeader({ title, actions = [] }: TitleHeaderProps) {
  const placement = useContext(HeaderPlacementContext);
  useNativeHeader(placement === "native" ? { title, large: true, actions } : null);
  if (placement !== "inline") return null;
  return <InlineTitleHeader title={title} actions={actions} />;
}

interface PushHeaderProps {
  title?: string;
  onBack: () => void;
  backLabel: string;
  actions?: readonly HeaderAction[];
}

/** Pushed screen header: back, an optional title, and actions. */
export function PushHeader({ title, onBack, backLabel, actions = [] }: PushHeaderProps) {
  const placement = useContext(HeaderPlacementContext);
  useNativeHeader(placement === "native" ? { title: title ?? "", large: false, actions } : placement === "detail" ? { title: "", large: false, actions } : null);
  if (placement !== "inline") return null;
  return <InlinePushHeader title={title} onBack={onBack} backLabel={backLabel} actions={actions} />;
}

/** A detail column with nothing in it still owns the bar, so the list's earlier actions go. */
export function EmptyDetailHeader() {
  const placement = useContext(HeaderPlacementContext);
  useNativeHeader(placement === "detail" ? { title: "", large: false, actions: [] } : null);
  return null;
}

function InlineTitleHeader({ title, actions }: { title: string; actions: readonly HeaderAction[] }) {
  const insets = useSafeAreaInsets();
  return (
    <View style={[styles.bar, { paddingTop: insets.top }]}>
      <View style={[styles.row, styles.title]}>
        <Txt variant="title" accessibilityRole="header" numberOfLines={1} style={styles.flex}>
          {title}
        </Txt>
        {actions.length ? <Capsule actions={actions} /> : null}
      </View>
    </View>
  );
}

function InlinePushHeader({ title, onBack, backLabel, actions }: { title?: string; onBack: () => void; backLabel: string; actions: readonly HeaderAction[] }) {
  const insets = useSafeAreaInsets();
  return (
    <View style={[styles.bar, { paddingTop: insets.top }]}>
      <View style={[styles.row, styles.push]}>
        <FloatingButton icon="ChevronLeft" label={backLabel} onPress={onBack} />
        <View style={styles.center}>
          {title ? (
            <Txt variant="navTitle" numberOfLines={1}>
              {title}
            </Txt>
          ) : null}
        </View>
        {actions.length ? <Capsule actions={actions} /> : <View style={styles.spacer} />}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: { zIndex: 10 },
  row: { height: 56, flexDirection: "row", alignItems: "center", gap: 12 },
  title: { paddingLeft: 20, paddingRight: 16 },
  push: { paddingHorizontal: 16 },
  flex: { flex: 1 },
  center: { flex: 1, alignItems: "center" },
  spacer: { width: 44 },
});
