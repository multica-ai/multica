import { Stack } from "expo-router";
import { colors } from "@/lib/theme";

export default function MoreLayout() {
  return (
    <Stack
      screenOptions={{
        contentStyle: { backgroundColor: colors.background },
        headerShown: false,
      }}
    />
  );
}
