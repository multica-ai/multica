import { View } from "react-native";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";

export default function AgentsPage() {
  const { t } = useTranslation("workspace");
  return (
    <View className="flex-1 items-center justify-center bg-background px-6">
      <Text className="text-sm text-muted-foreground text-center">
        {t("agents_placeholder.message")}
      </Text>
    </View>
  );
}
