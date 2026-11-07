// @vitest-environment node
import { act, useState, type ComponentProps } from "react";
import { create, type ReactTestRenderer, type ReactTestInstance } from "react-test-renderer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { InlineCommentComposer } from "@/components/issue/inline-comment-composer";
import { MessageComposer } from "./message-composer";
import { useMentionDraftStore } from "@/data/stores/mention-draft-store";
import type { MentionChip } from "@/components/issue/composer-attachment-row";

const native = vi.hoisted(() => ({
  blur: vi.fn(), dismiss: vi.fn(), uploadFile: vi.fn(), pickFile: vi.fn(), comment: vi.fn(),
}));
vi.mock("react-native", () => ({
  View: "View", Pressable: "Pressable", TextInput: "TextInput",
  Alert: { alert: vi.fn() }, Keyboard: { dismiss: native.dismiss },
}));
vi.mock("react-native-keyboard-controller", () => ({ KeyboardStickyView: "KeyboardStickyView" }));
vi.mock("react-native-safe-area-context", () => ({ useSafeAreaInsets: () => ({ bottom: 0 }) }));
vi.mock("@expo/vector-icons", () => ({ Ionicons: "Ionicons" }));
vi.mock("expo-router", () => ({ router: { push: vi.fn() }, DefaultTheme: {}, DarkTheme: {} }));
vi.mock("expo-haptics", () => ({
  impactAsync: async () => {}, selectionAsync: async () => {}, ImpactFeedbackStyle: { Light: "light" },
}));
vi.mock("expo-image-picker", () => ({ launchImageLibraryAsync: vi.fn(), MediaTypeOptions: { Images: "images" } }));
vi.mock("expo-document-picker", () => ({ getDocumentAsync: native.pickFile }));
vi.mock("@/data/api", () => ({ api: { uploadFile: native.uploadFile }, MAX_FILE_SIZE: 100000 }));
vi.mock("@/lib/use-color-scheme", () => ({ useColorScheme: () => ({ colorScheme: "light" }) }));
vi.mock("@/components/ui/text", () => ({ Text: "Text" }));
vi.mock("@/components/ui/icon-button", () => ({ IconButton: "IconButton" }));
vi.mock("@/components/issue/composer-attachment-row", () => ({ ComposerAttachmentRow: "AttachmentRow" }));
vi.mock("@/lib/i18n", () => ({ useT: () => ({ t: (key: string) => key }) }));

vi.mock("@/data/mutations/issues", () => ({ useCreateComment: () => ({ mutateAsync: native.comment }) }));
vi.mock("@/data/workspace-store", () => ({ useWorkspaceStore: (select: (s: { currentWorkspaceSlug: string }) => unknown) => select({ currentWorkspaceSlug: "test" }) }));

type Props = ComponentProps<typeof MessageComposer>;
const alice: MentionChip = { type: "member", id: "alice", name: "Alice" };
const bob: MentionChip = { type: "member", id: "bob", name: "Bob" };
function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
let renderer: ReactTestRenderer;
let mounted = false;
async function mount(extra: Partial<Props> = {}) {
  await act(async () => {
    renderer = create(<MessageComposer onSubmit={async () => {}} mentionPickerPath="/" {...extra} />, {
      createNodeMock: () => ({ blur: native.blur, focus: vi.fn(), isFocused: () => true }),
    });
    mounted = true;
  });
  await act(async () => { hosts("Pressable")[0].props.onPress(); });
}
// These names identify mocked host elements, not DOM/native elements.
const hosts = (type: string) => renderer.root.findAll((node: ReactTestInstance) => (node.type as unknown) === type);
const input = () => hosts("TextInput")[0];
const button = (name: string) => hosts("IconButton").find((node) => node.props.name === name)!;
const row = () => hosts("AttachmentRow")[0];
async function type(text: string) {
  await act(async () => { input().props.onChangeText(text); });
}
async function attach() {
  await act(async () => { await button("attach-outline").props.onPress(); });
}
async function settle(request: ReturnType<typeof deferred>, fail: boolean) {
  await act(async () => {
    if (fail) request.reject(new Error("offline")); else request.resolve();
    await request.promise.catch(() => {});
  });
}
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  vi.stubGlobal("requestAnimationFrame", () => 1);
  vi.stubGlobal("cancelAnimationFrame", vi.fn());
  vi.clearAllMocks();
  useMentionDraftStore.getState().clear();
  native.pickFile.mockResolvedValue({ canceled: false, assets: [{ uri: "file://a", name: "a.txt" }] });
  native.uploadFile.mockResolvedValue({ id: "file-a", url: "mc://file/a" });
});
afterEach(async () => {
  if (mounted) await act(async () => { renderer.unmount(); });
  mounted = false;
  vi.unstubAllGlobals();
});

describe("MessageComposer request lifecycle (mocked native hosts)", () => {
  it("prevents the A/send/B/failure overwrite through the real comment entry point", async () => {
    const request = deferred();
    native.comment.mockReturnValue(request.promise);
    await act(async () => { renderer = create(<InlineCommentComposer issueId="issue-a" />); mounted = true; });
    await act(async () => { hosts("Pressable")[0].props.onPress(); });
    await type("A");
    await act(async () => { void button("arrow-up").props.onPress(); });
    // A real native input emits edits only when editable. Pending sends
    // must either lock this input or preserve the next draft on settlement.
    if (input().props.editable) await type("B");
    const pendingText = input().props.value;
    await settle(request, true);
    expect(input().props.value).toBe(pendingText);
    expect(input().props.value).toBe("A");
    await type("B");
    expect(input().props.value).toBe("B");
    expect(native.comment).toHaveBeenCalledWith({ content: "A", parentId: undefined, attachmentIds: undefined });
  });

  it("retains text, attachment IDs and mentions for failure and clears only on acceptance", async () => {
    const request = deferred();
    const submit = vi.fn(() => request.promise);
    await mount({ onSubmit: submit });
    await type("  A  ");
    await attach();
    await act(async () => { useMentionDraftStore.getState().toggle(alice); });
    await act(async () => { void button("arrow-up").props.onPress(); });
    expect(submit).toHaveBeenCalledWith({ content: "[@Alice](mention://member/alice) A", attachmentIds: ["file-a"], mentions: [alice] });
    expect(input().props.value).toBe("  A  ");
    expect(input().props.editable).toBe(false);
    expect(row().props.attachments[0].id).toBe("file-a");
    await settle(request, true);
    expect(input().props.value).toBe("  A  ");
    expect(row().props.mentions).toEqual([alice]);
    expect(row().props.attachments[0].id).toBe("file-a");
    expect(native.dismiss).not.toHaveBeenCalled();
    submit.mockResolvedValueOnce();
    await act(async () => { await button("arrow-up").props.onPress(); });
    expect(hosts("TextInput")).toHaveLength(0);
    expect(useMentionDraftStore.getState().mentions).toEqual([]);
    expect(native.blur).toHaveBeenCalledOnce();
    expect(native.dismiss).toHaveBeenCalledOnce();
    await act(async () => { hosts("Pressable")[0].props.onPress(); });
    expect(input().props.value).toBe("");
    expect(hosts("AttachmentRow")).toHaveLength(0);
  });

  it("rejects duplicate and stale submit callbacks", async () => {
    const request = deferred();
    const submit = vi.fn(() => request.promise);
    await mount({ onSubmit: submit });
    await type("A");
    const sendA = button("arrow-up").props.onPress;
    await act(async () => { void sendA(); void sendA(); });
    expect(submit).toHaveBeenCalledOnce();
    await settle(request, true);
    await type("B");
    await act(async () => { await sendA(); });
    expect(submit).toHaveBeenCalledOnce();
    expect(input().props.value).toBe("B");
  });

  it.each([true, false])("does not touch a reopened composer's draft after old request settles (failure=%s)", async (fail) => {
    const request = deferred();
    const oldChange = vi.fn();
    await mount({ value: "A", onChangeText: oldChange, onSubmit: () => request.promise });
    await act(async () => { void button("arrow-up").props.onPress(); });
    oldChange.mockClear();
    await act(async () => { renderer.unmount(); });
    mounted = false;
    await mount({ value: "B", onChangeText: vi.fn() });
    await act(async () => { useMentionDraftStore.getState().toggle(bob); });
    await settle(request, fail);
    expect(oldChange).not.toHaveBeenCalled();
    expect(input().props.value).toBe("B");
    expect(useMentionDraftStore.getState().mentions).toEqual([bob]);
    expect(native.dismiss).not.toHaveBeenCalled();
  });

  it("does not clear a controlled draft replaced by its owner during a send", async () => {
    const request = deferred();
    let replace!: (text: string) => void;
    function Controlled() {
      const [value, setValue] = useState("A");
      replace = setValue;
      return <MessageComposer value={value} onChangeText={setValue} onSubmit={() => request.promise} mentionPickerPath="/" />;
    }
    await act(async () => { renderer = create(<Controlled />); mounted = true; });
    await act(async () => { hosts("Pressable")[0].props.onPress(); });
    await act(async () => { void button("arrow-up").props.onPress(); });
    await act(async () => { replace("B"); });
    await settle(request, false);
    expect(input().props.value).toBe("B");
    expect(native.dismiss).not.toHaveBeenCalled();
  });
  it("locks attachment/mention edits and ignores already-dispatched callbacks during send", async () => {
    const request = deferred();
    await mount({ onSubmit: () => request.promise });
    await type("A");
    await attach();
    await act(async () => { useMentionDraftStore.getState().toggle(alice); });
    const chips = row().props;
    const fileButton = button("attach-outline").props.onPress;
    const textChange = input().props.onChangeText;
    await act(async () => { void button("arrow-up").props.onPress(); });
    expect(row().props.disabled).toBe(true);
    for (const name of ["at", "image-outline", "attach-outline"]) expect(button(name).props.disabled).toBe(true);
    await act(async () => {
      textChange("B");
      chips.onRemoveMention("member", "alice");
      chips.onRemoveAttachment(chips.attachments[0].localId);
      chips.onRetryAttachment(chips.attachments[0].localId);
      await fileButton();
    });
    expect(native.pickFile).toHaveBeenCalledOnce();
    expect(native.uploadFile).toHaveBeenCalledOnce();
    expect(input().props.value).toBe("A");
    expect(row().props.mentions).toEqual([alice]);
    expect(row().props.attachments).toEqual(chips.attachments);
    await settle(request, true);
    expect(row().props.disabled).toBe(false);
    await type("B");
    await act(async () => { useMentionDraftStore.getState().toggle(bob); });
    native.uploadFile.mockResolvedValueOnce({ id: "file-b", url: "mc://file/b" });
    await attach();
    expect(input().props.value).toBe("B");
    expect(row().props.mentions).toEqual([alice, bob]);
    expect(row().props.attachments.map((item: { id: string }) => item.id)).toEqual(["file-a", "file-b"]);
  });

  it.each([true, false])("keeps mention picker changes during settlement without toggling old chips (failure=%s)", async (fail) => {
    const request = deferred();
    await mount({ onSubmit: () => request.promise });
    await type("A");
    await act(async () => { useMentionDraftStore.getState().toggle(alice); });
    await act(async () => { void button("arrow-up").props.onPress(); });
    await act(async () => { useMentionDraftStore.getState().toggle(bob); });
    await settle(request, fail);
    expect(input().props.value).toBe("A");
    expect(row().props.mentions).toEqual([alice, bob]);
    expect(native.dismiss).not.toHaveBeenCalled();
  });

  it("consumes accepted chips when the controlled owner already cleared the text", async () => {
    const request = deferred();
    let replace!: (text: string) => void;
    function Controlled() {
      const [value, setValue] = useState("A");
      replace = setValue;
      return <MessageComposer value={value} onChangeText={setValue} onSubmit={() => request.promise} mentionPickerPath="/" />;
    }
    await act(async () => { renderer = create(<Controlled />); mounted = true; });
    await act(async () => { hosts("Pressable")[0].props.onPress(); });
    await attach();
    await act(async () => { useMentionDraftStore.getState().toggle(alice); });
    await act(async () => { void button("arrow-up").props.onPress(); });
    await act(async () => { replace(""); });
    await settle(request, false);
    expect(hosts("TextInput")).toHaveLength(0);
    expect(useMentionDraftStore.getState().mentions).toEqual([]);
    await act(async () => { hosts("Pressable")[0].props.onPress(); });
    expect(hosts("AttachmentRow")).toHaveLength(0);
  });

  it("does not submit while an attachment is uploading", async () => {
    const upload = deferred();
    const submit = vi.fn(async () => {});
    native.uploadFile.mockImplementationOnce(async () => { await upload.promise; return { id: "file-a", url: "mc://file/a" }; });
    await mount({ onSubmit: submit });
    await type("A");
    let picking!: Promise<void>;
    await act(async () => { picking = button("attach-outline").props.onPress(); });
    expect(row().props.attachments[0].status).toBe("uploading");
    expect(button("arrow-up").props.disabled).toBe(true);
    await act(async () => { await button("arrow-up").props.onPress(); });
    expect(submit).not.toHaveBeenCalled();
    await act(async () => { upload.resolve(); await picking; });
    expect(button("arrow-up").props.disabled).toBe(false);
    await act(async () => { await button("arrow-up").props.onPress(); });
    expect(submit).toHaveBeenCalledWith({ content: "A", attachmentIds: ["file-a"], mentions: [] });
  });

  it("ignores a picker returning after the send has started", async () => {
    const picker = deferred();
    const request = deferred();
    native.pickFile.mockImplementationOnce(async () => {
      await picker.promise;
      return { canceled: false, assets: [{ uri: "file://b", name: "b.txt" }] };
    });
    await mount({ onSubmit: () => request.promise });
    await type("A");
    let picking!: Promise<void>;
    await act(async () => { picking = button("attach-outline").props.onPress(); });
    await act(async () => { void button("arrow-up").props.onPress(); });
    await act(async () => { picker.resolve(); await picking; });
    expect(native.uploadFile).not.toHaveBeenCalled();
    await settle(request, true);
    expect(input().props.value).toBe("A");
    expect(hosts("AttachmentRow")).toHaveLength(0);
  });

});
