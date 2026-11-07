import { SHEET_OPTIONS } from "@/lib/sheet-options";
import { Stack } from "expo-router";
import { AIConnectionProvider } from "@/data/ai-sessions/hooks";
import { useT } from "@/lib/i18n";

// Direct sheet links need the session list underneath the native modal.
export const unstable_settings = { anchor: "index" } as const;

export default function AIComputerLayout() {
  const { t } = useT("aiSessions");
  return <AIConnectionProvider><Stack screenOptions={{ headerBackTitle: t("sessions") }}>
    <Stack.Screen name="index" options={{ title: t("sessions"), headerBackTitle: t("title") }} />
    <Stack.Screen name="[session]" options={{ title: t("new") }} />
    <Stack.Screen name="folders" options={SHEET_OPTIONS} />
    {/* Settings owns its bounded content height; do not inherit the fixed-detent fill style. */}
    <Stack.Screen name="models" options={{ ...SHEET_OPTIONS, sheetAllowedDetents: "fitToContents", contentStyle: {} }} />
    {/* Search and paginated results need the larger detent. */}
    <Stack.Screen name="references" options={{ ...SHEET_OPTIONS, sheetInitialDetentIndex: 1 }} />
    {/* The queue owns its content height and must stay flush with the bottom edge on iOS 26. */}
    <Stack.Screen name="queue" options={{ presentation: "transparentModal", headerShown: false, animation: "fade", gestureEnabled: false, contentStyle: { backgroundColor: "transparent" } }} />
    <Stack.Screen name="usage" options={SHEET_OPTIONS} />
    <Stack.Screen name="files" options={{ title: t("files") }} />
    <Stack.Screen name="side" options={{ title: t("side") }} />
    <Stack.Screen name="side-sessions" options={SHEET_OPTIONS} />
  </Stack></AIConnectionProvider>;
}
