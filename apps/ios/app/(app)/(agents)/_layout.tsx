import { Stack } from "expo-router";
import { colors } from "@/lib/theme";

export default function AgentsLayout() {
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        contentStyle: { backgroundColor: colors.background },
        headerStyle: { backgroundColor: colors.background },
        headerTintColor: colors.foreground,
        headerShadowVisible: false,
      }}
    >
      <Stack.Screen name="index" options={{ title: "Agents" }} />
    </Stack>
  );
}
