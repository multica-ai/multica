import { Stack } from "expo-router";
import { colors } from "@/lib/theme";

export default function IssuesLayout() {
  return (
    <Stack
      screenOptions={{
        contentStyle: { backgroundColor: colors.background },
        headerStyle: { backgroundColor: colors.background },
        headerTintColor: colors.foreground,
        headerShadowVisible: false,
      }}
    >
      <Stack.Screen name="index" options={{ title: "Issues" }} />
      <Stack.Screen
        name="[id]"
        options={{
          headerShown: false,
        }}
      />
      <Stack.Screen
        name="create"
        options={{ title: "Create", presentation: "modal" }}
      />
    </Stack>
  );
}
