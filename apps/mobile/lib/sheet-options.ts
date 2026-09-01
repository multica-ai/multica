/**
 * Expo Router sheet presentation. iOS keeps formSheet (UISheetPresentationController).
 * Android formSheet falls back to a full-screen dialog with no grabber, so we
 * present a bottom-entering modal instead and rely on system back to dismiss.
 */

export type SheetPresentation = "formSheet" | "modal";

export type SheetScreenOptions = {
  presentation: SheetPresentation;
  sheetGrabberVisible?: boolean;
  sheetAllowedDetents?: number[] | "fitToContents";
  sheetCornerRadius?: number;
  animation?: "slide_from_bottom";
  gestureEnabled?: boolean;
  contentStyle: { flex: number };
  headerShown: false;
};

export function sheetScreenOptions(os: string): SheetScreenOptions {
  if (os === "ios") {
    return {
      presentation: "formSheet",
      sheetGrabberVisible: true,
      sheetAllowedDetents: [0.6, 0.95],
      sheetCornerRadius: 20,
      contentStyle: { flex: 1 },
      headerShown: false,
    };
  }
  return {
    presentation: "modal",
    animation: "slide_from_bottom",
    gestureEnabled: true,
    contentStyle: { flex: 1 },
    headerShown: false,
  };
}
