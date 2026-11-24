import { View } from "react-native";
import { Text } from "@/components/ui/text";
import { useT } from "@/lib/i18n";

export default function AgentsPage() {
  const { t } = useT("workspace");
  return (
    <View className="flex-1 items-center justify-center bg-background px-6">
      <Text className="text-sm text-muted-foreground text-center">
        {t("agent.coming_soon")}
      </Text>
    </View>
  );
}
