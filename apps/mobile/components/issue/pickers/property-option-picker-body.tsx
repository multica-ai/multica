/**
 * Pure picker body for a single-select custom property: its options, plus a
 * "no value" row unless the property is required (a required value can
 * change but not be cleared, as on web).
 */
import { Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type { IssueProperty } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { THEME } from "@/lib/theme";
import { useT } from "@/lib/i18n";

interface Props {
  property: IssueProperty;
  value: string | null;
  onChange: (next: string | null) => void;
}

export function PropertyOptionPickerBody({ property, value, onChange }: Props) {
  const { colorScheme } = useColorScheme();
  const { t } = useT("issues");
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;
  const rows: { id: string | null; name: string; color?: string }[] = [
    ...(property.required === true ? [] : [{ id: null, name: t("new.property_none") }]),
    ...(property.config.options ?? []).map((o) => ({ id: o.id, name: o.name, color: o.color })),
  ];

  return (
    <ScrollView showsVerticalScrollIndicator={false}>
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground" numberOfLines={1}>
          {property.name}
        </Text>
      </View>
      <View className="px-2">
        {rows.map((row) => (
          <Pressable
            key={row.id ?? "none"}
            onPress={() => onChange(row.id)}
            className="flex-row items-center gap-3 rounded-lg px-3 py-3 active:bg-secondary"
          >
            {row.color ? (
              <View className="size-2.5 rounded-full" style={{ backgroundColor: row.color }} />
            ) : (
              <View className="size-2.5" />
            )}
            <Text className="flex-1 text-base text-foreground" numberOfLines={1}>
              {row.name}
            </Text>
            {row.id === value ? (
              <Ionicons name="checkmark" size={20} color={checkColor} />
            ) : null}
          </Pressable>
        ))}
      </View>
    </ScrollView>
  );
}
