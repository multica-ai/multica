/**
 * HarmonyOS port of apps/mobile/components/ui/radio-group.tsx. The iOS
 * version wraps @rn-primitives/radio-group; the primitives are unavailable
 * on RNOH and NativeWind className styling regressed, so this is a pure-RN
 * equivalent with the same composable API:
 *
 *   <RadioGroup value={v} onValueChange={setV}>
 *     <RadioGroupItem value="a" />
 *
 * Visual values are the tailwind defaults: group gap-3 (12); item
 * size-4 (16) rounded-full with a 1px input border, 8px primary dot
 * indicator when selected, disabled opacity-50.
 */
import { createContext, useContext } from "react";
import {
  Pressable,
  StyleSheet,
  View,
  type ViewProps,
} from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";

interface RadioGroupContextValue {
  value: string;
  onValueChange: (value: string) => void;
}

const RadioGroupContext = createContext<RadioGroupContextValue | null>(null);

function RadioGroup({
  value,
  onValueChange,
  style,
  ...props
}: ViewProps & {
  value: string;
  onValueChange: (value: string) => void;
}) {
  return (
    <RadioGroupContext.Provider value={{ value, onValueChange }}>
      <View style={[styles.group, style]} {...props} />
    </RadioGroupContext.Provider>
  );
}

function RadioGroupItem({
  value,
  disabled,
  style,
  ...props
}: Omit<ViewProps, "children"> & {
  value: string;
  disabled?: boolean;
}) {
  const c = useThemeColors();
  const group = useContext(RadioGroupContext);
  const selected = group?.value === value;

  const press = () => {
    if (!disabled) group?.onValueChange(value);
  };

  return (
    <Pressable
      role="radio"
      accessibilityState={{ selected, disabled: !!disabled }}
      onPress={press}
      style={[
        styles.item,
        { borderColor: c.input },
        disabled ? styles.disabled : null,
        style,
      ]}
      {...props}
    >
      {selected ? (
        <View style={[styles.indicator, { backgroundColor: c.primary }]} />
      ) : null}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  group: { gap: 12 },
  item: {
    width: 16,
    height: 16,
    borderRadius: 9999,
    borderWidth: 1,
    alignItems: "center",
    justifyContent: "center",
  },
  indicator: { width: 8, height: 8, borderRadius: 9999 },
  disabled: { opacity: 0.5 },
});

export { RadioGroup, RadioGroupItem };
