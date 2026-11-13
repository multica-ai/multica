import { useState, type ReactNode } from "react";
import { Modal, Pressable, StyleSheet, View } from "react-native";
import { BlurView } from "expo-blur";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { FloatingButton, FloatingSurface } from "../components/Floating";
import { Txt } from "../components/Text";
import { Icon, type IconName } from "../icons/Icon";
import { useStrings } from "../strings";
import { useTheme } from "../theme";

export interface MenuWorkspace {
  slug: string;
  name: string;
}

export interface MenuDestination {
  key: string;
  icon: IconName;
  label: string;
  meta?: string;
}

export interface MenuViews {
  items: readonly MenuDestination[];
  onAll: () => void;
}

interface WorkspaceMenuBodyProps {
  workspace: MenuWorkspace;
  workspaces: readonly MenuWorkspace[];
  /** Groups of destinations, split by a hairline. */
  sections: readonly (readonly MenuDestination[])[];
  /** Starred views, in a card of their own under the destinations. */
  views?: MenuViews;
  active: string;
  onNavigate: (key: string) => void;
  onSwitch: (slug: string) => void;
  onSettings: () => void;
  initialMode?: "nav" | "workspaces";
}

function WorkspaceMark({ name }: { name: string }) {
  const palette = useTheme();
  return (
    <View style={[styles.mark, { backgroundColor: palette.label }]}>
      <Txt variant="caption" style={{ color: palette.background }}>
        {name.trim().slice(0, 1).toUpperCase() || "?"}
      </Txt>
    </View>
  );
}

interface MenuRowProps {
  icon?: IconName;
  label: string;
  meta?: string;
  checked?: boolean;
  selected: boolean;
  onPress: () => void;
  leading?: ReactNode;
}

function MenuRow({ icon, label, meta, checked = false, selected, onPress, leading }: MenuRowProps) {
  const palette = useTheme();
  return (
    <Pressable
      accessibilityRole="menuitem"
      accessibilityState={{ selected }}
      onPress={onPress}
      style={({ pressed }) => [styles.item, (selected || pressed) && { backgroundColor: palette.fill }]}
    >
      {leading ?? (icon ? <Icon name={icon} size={19} color={palette.label} /> : null)}
      <Txt variant="row" numberOfLines={1} style={styles.flex}>
        {label}
      </Txt>
      {meta ? (
        <Txt variant="meta" tone="tertiary">
          {meta}
        </Txt>
      ) : null}
      {checked ? <Icon name="Check" size={17} color={palette.label} /> : null}
    </Pressable>
  );
}

/** The menu itself, without the modal, so the preview can render it in place. */
export function WorkspaceMenuBody({ workspace, workspaces, sections, views, active, onNavigate, onSwitch, onSettings, initialMode = "nav" }: WorkspaceMenuBodyProps) {
  const strings = useStrings();
  const palette = useTheme();
  const [mode, setMode] = useState(initialMode);
  return (
    <View style={styles.stack}>
      <View style={styles.top}>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={strings.more.switchWorkspace}
          onPress={() => setMode(mode === "nav" ? "workspaces" : "nav")}
          style={({ pressed }) => ({ opacity: pressed ? 0.7 : 1 })}
        >
          <FloatingSurface style={styles.pill}>
            <WorkspaceMark name={workspace.name} />
            <Txt variant="calloutStrong" numberOfLines={1} style={styles.pillName}>
              {workspace.name}
            </Txt>
            <Icon name="ChevronsUpDown" size={15} color={palette.label2} />
          </FloatingSurface>
        </Pressable>
        <FloatingButton icon="Settings" label={strings.more.settings} onPress={onSettings} />
      </View>
      <FloatingSurface style={styles.card}>
        {mode === "nav"
          ? sections.map((section, index) => (
              <View key={index}>
                {index > 0 ? <View style={[styles.rule, { backgroundColor: palette.separator }]} /> : null}
                {section.map((item) => (
                  <MenuRow key={item.key} icon={item.icon} label={item.label} meta={item.meta} selected={item.key === active} onPress={() => onNavigate(item.key)} />
                ))}
              </View>
            ))
          : workspaces.map((item) => (
              <MenuRow
                key={item.slug}
                label={item.name}
                selected={false}
                checked={item.slug === workspace.slug}
                leading={<WorkspaceMark name={item.name} />}
                onPress={() => onSwitch(item.slug)}
              />
            ))}
      </FloatingSurface>
      {mode === "nav" && views ? (
        <FloatingSurface style={styles.card}>
          <View style={styles.cardHead}>
            <Txt variant="caption" tone="tertiary" style={styles.flex}>
              {strings.nav.views}
            </Txt>
            <Pressable accessibilityRole="button" hitSlop={10} onPress={views.onAll}>
              <Txt variant="caption" tone="tertiary">
                {strings.nav.all}
              </Txt>
            </Pressable>
          </View>
          {views.items.map((item) => (
            <MenuRow key={item.key} icon={item.icon} label={item.label} meta={item.meta} selected={item.key === active} onPress={() => onNavigate(item.key)} />
          ))}
        </FloatingSurface>
      ) : null}
    </View>
  );
}

interface WorkspaceMenuProps extends WorkspaceMenuBodyProps {
  visible: boolean;
  onClose: () => void;
  /** Stays sharp above the blur: the tab bar that opened the menu. */
  children?: ReactNode;
}

/** Opened by the fourth tab: every destination and the workspace switcher, over a blurred screen. */
export function WorkspaceMenu({ visible, onClose, children, ...body }: WorkspaceMenuProps) {
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose} statusBarTranslucent>
      <BlurView intensity={28} tint={palette.scheme === "dark" ? "dark" : "light"} style={StyleSheet.absoluteFill} />
      <Pressable accessibilityLabel={useStrings().common.close} style={[StyleSheet.absoluteFill, { backgroundColor: palette.scrim }]} onPress={onClose} />
      <View style={[styles.anchor, { bottom: Math.max(insets.bottom - 8, 12) + 68 }]} pointerEvents="box-none">
        <WorkspaceMenuBody {...body} />
      </View>
      {children}
    </Modal>
  );
}

const styles = StyleSheet.create({
  anchor: { position: "absolute", left: 16 },
  stack: { gap: 10, width: 262 },
  top: { flexDirection: "row", alignItems: "center", gap: 10 },
  pill: { height: 44, borderRadius: 22, flexDirection: "row", alignItems: "center", gap: 8, paddingLeft: 8, paddingRight: 12, maxWidth: 200 },
  pillName: { flexShrink: 1 },
  mark: { width: 26, height: 26, borderRadius: 7, alignItems: "center", justifyContent: "center" },
  card: { borderRadius: 22, padding: 6 },
  cardHead: { flexDirection: "row", paddingHorizontal: 12, paddingTop: 6, paddingBottom: 2 },
  rule: { height: StyleSheet.hairlineWidth, marginHorizontal: 12, marginVertical: 4 },
  item: { height: 44, borderRadius: 14, flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 12 },
  flex: { flex: 1 },
});
