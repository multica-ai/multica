/**
 * HarmonyOS port of apps/mobile/components/ui/tabs.tsx — the component-
 * library tabs primitive (distinct from the app's tab bar in
 * src/navigation/tabs.tsx). The iOS version wraps @rn-primitives/tabs with
 * NativeWind classes; the primitives are unavailable on RNOH, so this is a
 * pure-RN equivalent with the same composable API:
 *
 *   <Tabs value={v} onValueChange={setV}>
 *     <TabsList><TabsTrigger value="a" /><TabsTrigger value="b" /></TabsList>
 *     <TabsContent value="a">…</TabsContent>
 *
 * Visual values are the tailwind defaults: root flex-col gap-2 (8); list
 * bg-muted h-9 (36) rounded-lg (8) p-[3px], left-aligned on native
 * (mr-auto); trigger px-2 py-1 gap-1.5 rounded-md (6), selected trigger
 * bg-background. Label styling flows through TextClassContext (14/500);
 * the iOS quirk is kept faithful — in light mode every label is
 * foreground, in dark mode unselected labels mute to mutedForeground.
 * Content unmounts when inactive (the primitive's no-forceMount default).
 */
import React, { createContext, useContext } from "react";
import {
  Pressable,
  StyleSheet,
  Text as RNText,
  View,
  useColorScheme,
  type PressableProps,
  type StyleProp,
  type TextStyle,
  type ViewProps,
  type ViewStyle,
} from "react-native";
import { TextClassContext } from "@/components/ui/text";
import { useThemeColors } from "@/lib/use-theme-colors";

interface TabsContextValue {
  value: string;
  onValueChange: (value: string) => void;
}

const TabsContext = createContext<TabsContextValue | null>(null);

function Tabs({
  value,
  onValueChange,
  ...props
}: ViewProps & {
  value: string;
  onValueChange: (value: string) => void;
}) {
  return (
    <TabsContext.Provider value={{ value, onValueChange }}>
      <View style={styles.root} {...props} />
    </TabsContext.Provider>
  );
}

function TabsList({ style, ...props }: ViewProps) {
  const c = useThemeColors();
  return (
    <View
      style={[
        styles.list,
        { backgroundColor: c.muted },
        style,
      ]}
      {...props}
    />
  );
}

function TabsTrigger({
  value,
  disabled,
  style,
  children,
  ...props
}: Omit<PressableProps, "children" | "style"> & {
  value: string;
  style?: StyleProp<ViewStyle>;
  children?: React.ReactNode;
}) {
  const c = useThemeColors();
  const isDark = useColorScheme() === "dark";
  const root = useContext(TabsContext);
  const selected = root?.value === value;

  // Faithful port of the iOS label classes: text-foreground +
  // dark:text-muted-foreground, with dark:text-foreground when selected.
  const labelStyle: StyleProp<TextStyle> = isDark
    ? { color: selected ? c.foreground : c.mutedForeground }
    : { color: c.foreground };

  return (
    <TextClassContext.Provider value={labelStyle}>
      <Pressable
        role="tab"
        accessibilityState={{ selected, disabled: !!disabled }}
        disabled={disabled}
        onPress={() => root?.onValueChange(value)}
        style={[
          styles.trigger,
          { backgroundColor: selected ? c.background : "transparent" },
          disabled ? styles.disabled : null,
          style,
        ]}
        {...props}
      >
        {typeof children === "string" ? (
          <RNText style={[styles.triggerLabel, labelStyle]}>{children}</RNText>
        ) : (
          children
        )}
      </Pressable>
    </TextClassContext.Provider>
  );
}

function TabsContent({
  value,
  style,
  children,
  ...props
}: ViewProps & {
  value: string;
  children?: React.ReactNode;
}) {
  const root = useContext(TabsContext);
  if (root?.value !== value) return null;
  return (
    <View style={[styles.content, style]} {...props}>
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flexDirection: "column", gap: 8 },
  list: {
    height: 36,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 8,
    padding: 3,
    alignSelf: "flex-start",
  },
  trigger: {
    height: "100%",
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    borderRadius: 6,
    paddingHorizontal: 8,
    paddingVertical: 4,
  },
  triggerLabel: { fontSize: 14, fontWeight: "500" },
  content: { flex: 1 },
  disabled: { opacity: 0.5 },
});

export { Tabs, TabsContent, TabsList, TabsTrigger };
