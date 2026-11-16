// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { useActionSheet } from "./use-action-sheet";

const native = vi.hoisted(() => ({ os: "android", select: undefined as ((index?: number) => void) | undefined }));
vi.mock("react", () => ({ useCallback: (callback: unknown) => callback }));
vi.mock("react-native", () => ({ Platform: { get OS() { return native.os; } } }));
vi.mock("react-native-safe-area-context", () => ({ useSafeAreaInsets: () => ({ bottom: 0 }) }));
vi.mock("@/lib/use-color-scheme", () => ({ useColorScheme: () => ({ colorScheme: "light" }) }));
vi.mock("@expo/react-native-action-sheet", () => ({
  useActionSheet: () => ({
    showActionSheetWithOptions: (_options: unknown, select: (index?: number) => void) => { native.select = select; },
  }),
}));
vi.mock("@/lib/theme", () => ({ THEME: { light: { foreground: "black", destructive: "red", popover: "white" } } }));
afterEach(() => vi.useRealTimers());

it.each(["android", "ios"])("runs %s selections after its platform's dismissal boundary", (os) => {
  native.os = os;
  vi.useFakeTimers();
  const selected = vi.fn();
  useActionSheet().showActionSheetWithOptions({ options: ["React", "Cancel"], cancelButtonIndex: 1 }, selected);
  native.select!(0);
  if (os === "android") expect(selected).not.toHaveBeenCalled();
  else expect(selected).toHaveBeenCalledWith(0);
  vi.runAllTimers();
  expect(selected).toHaveBeenCalledOnce();
  native.select!();
  vi.runAllTimers();
  expect(selected).toHaveBeenLastCalledWith(1);
});
