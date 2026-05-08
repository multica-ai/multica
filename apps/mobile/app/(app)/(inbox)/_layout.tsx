import { Stack } from "expo-router";

// Index uses a custom in-screen header (see components/ui/screen-header).
// Detail keeps the iOS Stack header for free swipe-back + native `<` button.
export default function InboxStackLayout() {
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
