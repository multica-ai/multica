// @vitest-environment node
import type { ReactElement, ReactNode } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { MessageComposer } from "./message-composer";
import { invalidateSessionEpoch } from "@/data/session-epoch";
import { useMentionDraftStore } from "@/data/stores/mention-draft-store";
import { useChatDraftsStore } from "@/data/stores/chat-drafts-store";

const hooks = vi.hoisted(() => ({ setters: [] as ReturnType<typeof vi.fn>[] }));
vi.mock("react", async (original) => ({
  ...await original<typeof import("react")>(),
  useCallback: (fn: unknown) => fn,
  useEffect: () => {},
  useRef: () => ({ current: null }),
  useState: (initial: unknown) => {
    const n = hooks.setters.length;
    const set = vi.fn();
    hooks.setters.push(set);
    return [n === 0 ? true : initial, set];
  },
}));
vi.mock("react-native", () => ({
  Alert: {}, Keyboard: { dismiss: vi.fn() }, Pressable: "Pressable", TextInput: "TextInput", View: "View",
}));
vi.mock("react-native-keyboard-controller", () => ({ KeyboardStickyView: "KeyboardStickyView" }));
vi.mock("react-native-safe-area-context", () => ({ useSafeAreaInsets: () => ({ bottom: 0 }) }));
vi.mock("@expo/vector-icons", () => ({ Ionicons: "Ionicons" }));
vi.mock("expo-router", () => ({ router: { push: vi.fn() } }));
vi.mock("expo-haptics", () => ({ impactAsync: async () => {}, ImpactFeedbackStyle: { Light: "light" } }));
vi.mock("expo-image-picker", () => ({}));
vi.mock("expo-document-picker", () => ({}));
vi.mock("@/data/api", () => ({ api: {}, MAX_FILE_SIZE: 1000 }));
vi.mock("@/components/ui/text", () => ({ Text: "Text" }));
vi.mock("@/components/ui/icon-button", () => ({ IconButton: "IconButton" }));
vi.mock("@/components/issue/composer-attachment-row", () => ({ ComposerAttachmentRow: "ComposerAttachmentRow" }));
vi.mock("@/lib/use-color-scheme", () => ({ useColorScheme: () => ({ colorScheme: "light" }) }));
vi.mock("@/lib/theme", () => ({ THEME: { light: { mutedForeground: "gray", primaryForeground: "white" } } }));
vi.mock("@/lib/i18n", () => ({ useT: () => ({ t: (key: string) => key }) }));
vi.mock("@/data/stores/mention-draft-store", async (original) => {
  const { useMentionDraftStore: store } = await original<typeof import("@/data/stores/mention-draft-store")>();
  return { useMentionDraftStore: Object.assign((select: (s: ReturnType<typeof store.getState>) => unknown) => select(store.getState()), store) };
});

function findSend(node: ReactNode): ReactElement<{ onPress: () => void }> | undefined {
  if (!node || typeof node !== "object" || !("props" in node)) return;
  const element = node as ReactElement<{ accessibilityLabel?: string; children?: ReactNode; onPress: () => void }>;
  if (element.props.accessibilityLabel === "composer.send") return element;
  for (const child of [element.props.children].flat()) {
    const found = findSend(child);
    if (found) return found;
  }
}

beforeEach(() => {
  hooks.setters = [];
  useMentionDraftStore.getState().clear();
  useChatDraftsStore.setState({ drafts: {} });
});

// Invoke the real composer's submit callback; native drawing and hooks are
// substituted. The real draft/mention stores verify its async side effects.
it.each([false, true])("restores a failed draft only in its originating session (switched=%s)", async (switched) => {
  const mention = { type: "member" as const, id: "private-member", name: "Synthetic" };
  useMentionDraftStore.getState().toggle(mention);
  useChatDraftsStore.getState().setDraft("session", "private draft");
  let reject!: (error: Error) => void;
  const request = new Promise<void>((_resolve, no) => { reject = no; });
  const tree = MessageComposer({
    value: "private draft",
    onChangeText: (text) => useChatDraftsStore.getState().setDraft("session", text),
    onSubmit: () => request,
    mentionPickerPath: "/select-workspace",
    manageKeyboard: false,
  });
  findSend(tree)!.props.onPress();
  expect(useMentionDraftStore.getState().mentions).toEqual([]);
  if (switched) {
    invalidateSessionEpoch();
    useChatDraftsStore.setState({ drafts: {} });
    useMentionDraftStore.getState().clear();
  }
  reject(new Error("Send failed"));
  await new Promise((done) => setTimeout(done, 0));
  expect(useChatDraftsStore.getState().drafts).toEqual(switched ? {} : { session: "private draft" });
  expect(useMentionDraftStore.getState().mentions).toEqual(switched ? [] : [mention]);
});
