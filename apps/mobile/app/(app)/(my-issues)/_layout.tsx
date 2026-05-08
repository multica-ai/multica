import { Stack } from "expo-router";

export default function MyIssuesStackLayout() {
  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen
        name="issue/[id]"
        options={{ headerShown: false }}
      />
    </Stack>
  );
}
