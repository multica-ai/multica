import { Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import type { RuntimeDevice } from "@multica/core/types";
import { deriveRuntimeHealth } from "@multica/core/runtimes";
import { Text } from "@/components/ui/text";
import { ProviderLogo } from "@/components/runtime/provider-logo";
import { HEALTH_DOT_CLASS } from "@/lib/runtime-health";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  runtime: RuntimeDevice;
  onPress: () => void;
}

export function RuntimeRow({ runtime, onPress }: Props) {
  const { t } = useTranslation("runtimes");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const health = deriveRuntimeHealth(runtime, Date.now());

  return (
    <Pressable onPress={onPress} className="active:bg-secondary px-4 py-3">
      <View className="flex-row items-start gap-3">
        <View style={{ marginTop: 2 }}>
          <ProviderLogo provider={runtime.provider} size={20} color={mutedFg} />
        </View>
        <View className="flex-1 gap-1">
          <Text
            className="text-base text-foreground font-medium"
            numberOfLines={1}
          >
            {runtime.name}
          </Text>
          <View className="flex-row items-center gap-1.5">
            <View
              className={`size-2 rounded-full ${HEALTH_DOT_CLASS[health]}`}
            />
            <Text className="text-xs text-muted-foreground">
              {t(`health.${health}.label`)}
            </Text>
          </View>
        </View>
        <View className="items-end gap-1">
          <Ionicons
            name={
              runtime.visibility === "public"
                ? "globe-outline"
                : "lock-closed-outline"
            }
            size={14}
            color={mutedFg}
          />
          <Text className="text-[11px] text-muted-foreground/70">
            {timeAgo(runtime.last_seen_at ?? runtime.updated_at)}
          </Text>
        </View>
      </View>
    </Pressable>
  );
}
