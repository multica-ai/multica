/**
 * Chat tab stack. Like inbox/ and my-issues/, wrapping the screen in a native
 * Stack is what gives it a real iOS nav bar (blur material, native title
 * treatment) instead of the JS `<Header>` bar.
 *
 * Unlike those list tabs, Chat is a *conversation* view, so it uses an inline
 * title — NOT `headerLargeTitle`. The title (session switcher) and the
 * new-chat / delete actions are set dynamically from the screen via
 * `<Stack.Screen options>` so they can read the active session + handlers.
 */
import { Stack } from "expo-router";

export default function ChatStackLayout() {
  return (
    <Stack screenOptions={{ headerShadowVisible: false }}>
      <Stack.Screen name="index" />
    </Stack>
  );
}
