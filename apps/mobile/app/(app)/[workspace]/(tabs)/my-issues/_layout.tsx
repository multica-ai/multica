/**
 * My Issues tab stack. See inbox/_layout.tsx — wrapping the screen in a
 * native Stack is what unlocks the iOS large-title nav bar (a
 * react-native-screens / UINavigationController feature the JS `<Tabs>`
 * header can't provide). The scope toolbar stays pinned below the bar; the
 * SectionList drives the large-title → inline collapse via
 * `contentInsetAdjustmentBehavior="automatic"`.
 */
import { Stack } from "expo-router";

export default function MyIssuesStackLayout() {
  return (
    <Stack
      screenOptions={{
        headerLargeTitle: true,
        headerLargeTitleShadowVisible: false,
        headerShadowVisible: false,
      }}
    >
      <Stack.Screen name="index" options={{ title: "My Issues" }} />
    </Stack>
  );
}
