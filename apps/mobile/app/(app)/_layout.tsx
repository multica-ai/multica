import { Redirect, Tabs, useSegments } from "expo-router";
import { useAuthStore } from "@multica/core/auth";

import { IconSymbol } from "@/components/ui/icon-symbol";
import { WorkspaceBootstrap } from "@/components/workspace/workspace-bootstrap";

// We use `Tabs` (JavaScript implementation, backed by
// @react-navigation/bottom-tabs) instead of `NativeTabs` from
// `expo-router/unstable-native-tabs`.
//
// WHY NOT NativeTabs:
//   NativeTabs (SDK 54 alpha) wraps the native iOS UITabBarController, which
//   gives the prettiest visual fidelity (real UITabBar blur, system colors).
//   But the alpha API doesn't yet expose `hidesBottomBarWhenPushed` —
//   the iOS-native flag that auto-hides the tab bar when a screen is pushed
//   into a stack. Without it, the tab bar stays visible on issue detail
//   views, where it (a) steals ~83pt of bottom space from the composer +
//   safe area, and (b) is semantically wrong: issue detail is a focus view,
//   not a navigation hub. Open issues: expo/router #518 / discussion #313.
//
// WHY Tabs IS BETTER FOR US:
//   `@react-navigation/bottom-tabs` is 5+ years stable, supports per-route
//   `tabBarStyle` overrides — including `display: 'none'`. We compute a
//   `hideTabBar` flag from `useSegments()` and apply `tabBarStyle` to make
//   the tab bar disappear whenever the user is inside an `issue/[id]` route.
//   Same UX outcome as Linear's iOS app, achieved through the canonical RN
//   pattern.
//
// VISUAL TRADE-OFF:
//   Tabs uses a JS-rendered tab bar (View + Pressable) instead of native
//   UITabBar. Visual quality is ~95% there: SF Symbols for icons, brand
//   color for active tint, light-gray for inactive. We lose the live
//   UIKit blur but gain the hide-on-push behavior — a fair trade.
export default function AppLayout() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const segments = useSegments();

  // segments is e.g. ['(app)', '(inbox)', 'issue', '[id]'] when viewing an
  // issue. Hide the tab bar whenever 'issue' appears anywhere in the path.
  // Matches Linear's "tab bar disappears in detail view" pattern.
  const hideTabBar = segments.some((s) => s === "issue");

  if (isLoading) return null;
  if (!user) return <Redirect href="/(auth)/login" />;

  return (
    <WorkspaceBootstrap>
      <Tabs
        screenOptions={{
          headerShown: false,
          tabBarActiveTintColor: "hsl(220 60% 50%)", // brand
          tabBarInactiveTintColor: "hsl(240 4% 46%)", // muted-foreground
          tabBarStyle: hideTabBar
            ? { display: "none" }
            : { borderTopColor: "hsl(240 6% 90%)" }, // border token
          tabBarLabelStyle: { fontSize: 11 },
        }}
      >
        <Tabs.Screen
          name="(inbox)"
          options={{
            title: "Inbox",
            tabBarIcon: ({ color, size }) => (
              // eslint-disable-next-line @typescript-eslint/no-explicit-any
              <IconSymbol name={"tray.fill" as any} size={size} color={color} />
            ),
          }}
        />
        <Tabs.Screen
          name="(my-issues)"
          options={{
            title: "My Issues",
            tabBarIcon: ({ color, size }) => (
              // eslint-disable-next-line @typescript-eslint/no-explicit-any
              <IconSymbol
                name={"checkmark.circle" as any}
                size={size}
                color={color}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="(me)"
          options={{
            title: "Me",
            tabBarIcon: ({ color, size }) => (
              // eslint-disable-next-line @typescript-eslint/no-explicit-any
              <IconSymbol
                name={"person.crop.circle" as any}
                size={size}
                color={color}
              />
            ),
          }}
        />
      </Tabs>
    </WorkspaceBootstrap>
  );
}
