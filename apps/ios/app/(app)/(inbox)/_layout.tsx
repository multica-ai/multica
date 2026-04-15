import { Stack } from "expo-router";
import { colors } from "@/lib/theme";

export default function InboxLayout() {
  return (
    <Stack
      screenOptions={{
        contentStyle: { backgroundColor: colors.background },
        headerStyle: { backgroundColor: colors.background },
        headerTintColor: colors.foreground,
        headerShadowVisible: false,
      }}
    >
      <Stack.Screen name="index" options={{ title: "Inbox" }} />
    </Stack>
  );
}
