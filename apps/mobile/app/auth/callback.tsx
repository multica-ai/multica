import { ActivityIndicator, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { MulticaLogo } from "@/components/brand/multica-logo";
import { Text } from "@/components/ui/text";

export default function AuthCallback() {
  return (
    <SafeAreaView className="flex-1 bg-background">
      <View className="flex-1 items-center justify-center gap-4 px-6">
        <MulticaLogo size={32} />
        <Text className="text-xl font-semibold text-foreground">
          Completing sign-in
        </Text>
        <ActivityIndicator />
      </View>
    </SafeAreaView>
  );
}
