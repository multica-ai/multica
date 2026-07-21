/**
 * Inbox tab stack. Wrapping the inbox screen in a native Stack (instead of
 * mounting it directly under the JS `<Tabs>`) is what unlocks the iOS
 * large-title nav bar: `headerLargeTitle` is a UINavigationController
 * feature exposed only by react-native-screens' native stack — the
 * react-navigation bottom-tabs header (JS) can't render it.
 *
 * The list opts into the "large title collapses to inline on scroll"
 * behaviour via `contentInsetAdjustmentBehavior="automatic"`.
 */
import { Stack } from "expo-router";

export default function InboxStackLayout() {
  return (
    <Stack
      screenOptions={{
        headerLargeTitle: true,
        headerLargeTitleShadowVisible: false,
        headerShadowVisible: false,
      }}
    >
      <Stack.Screen name="index" options={{ title: "Inbox" }} />
    </Stack>
  );
}
