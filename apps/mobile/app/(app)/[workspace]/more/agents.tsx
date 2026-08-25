import { View } from "react-native";
import { Text } from "@/components/ui/text";
import { translate } from "@/i18n";

export default function AgentsPage() {
  return (
    <View className="flex-1 items-center justify-center bg-background px-6">
      <Text className="text-sm text-muted-foreground text-center">
        {translate("Agents coming soon.")}
      </Text>
    </View>
  );
}
