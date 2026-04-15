import { Stack } from "expo-router";
import { colors } from "@/lib/theme";

export default function ProjectsLayout() {
  return (
    <Stack
      screenOptions={{
        contentStyle: { backgroundColor: colors.background },
        headerShown: false,
      }}
    />
  );
}
