import { Ionicons } from "@expo/vector-icons";
import { View } from "react-native";
import { Text } from "@/components/ui/text";
import { translate } from "@/i18n";

export function RuntimeRequiredBanner({ agentName }: { agentName?: string }) {
  const name = agentName?.trim() || translate("This agent");
  return (
    <View className="mx-3 mb-1.5 flex-row items-center gap-1.5 rounded-md bg-warning/15 px-2.5 py-1.5">
      <Ionicons name="server-outline" size={14} color="#a16207" />
      <Text className="flex-1 text-xs text-warning">
        {translate(
          "{{name}} needs a runtime before it can run. Bind one on web or desktop.",
          { name },
        )}
      </Text>
    </View>
  );
}
