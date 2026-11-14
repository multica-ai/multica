import { useState } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import type { RunStep } from "../model/steps";
import { Icon } from "../icons/Icon";
import { useTheme } from "../theme";
import { RunSteps } from "./RunSteps";
import { Txt } from "./Text";

interface StepsDisclosureProps {
  label: string;
  /** Loaded when first opened. */
  steps: readonly RunStep[] | null;
  onOpen: () => void;
}

/** "12 steps ›": a finished run's tool calls, folded away under the reply. */
export function StepsDisclosure({ label, steps, onOpen }: StepsDisclosureProps) {
  const palette = useTheme();
  const [open, setOpen] = useState(false);
  return (
    <View style={styles.box}>
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ expanded: open }}
        onPress={() => {
          if (!open) onOpen();
          setOpen(!open);
        }}
        hitSlop={6}
        style={styles.head}
      >
        <Txt variant="meta" tone="secondary">
          {label}
        </Txt>
        <Icon name={open ? "ChevronDown" : "ChevronRight"} size={14} color={palette.label3} />
      </Pressable>
      {open && steps ? <RunSteps steps={steps} /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: 8 },
  head: { flexDirection: "row", alignItems: "center", gap: 4, alignSelf: "flex-start" },
});
