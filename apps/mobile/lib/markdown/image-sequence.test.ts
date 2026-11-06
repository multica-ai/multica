// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Attachment } from "@multica/core/types";

const state = vi.hoisted(() => ({
  contexts: new Map<unknown, unknown>(),
  setters: [] as ReturnType<typeof vi.fn>[],
}));

vi.mock("react", async (importOriginal) => ({
  ...await importOriginal<typeof import("react")>(),
  use: (context: unknown) => state.contexts.get(context),
  useMemo: (factory: () => unknown) => factory(),
  useEffect: vi.fn(),
  useState: (initial: unknown) => {
    const setter = vi.fn();
    state.setters.push(setter);
    return [initial, setter];
  },
}));
vi.mock("react-native", () => ({
  Image: { getSize: vi.fn() }, Pressable: "Pressable", View: "View",
}));
vi.mock("expo-image", () => ({ Image: "ExpoImage" }));
vi.mock("react-native-image-viewing", () => ({ default: "ImageView" }));
vi.mock("@/components/ui/text", () => ({ Text: "Text" }));

import { preprocessMobileMarkdown } from "./preprocess";
import { splitMarkdown } from "./split-markdown";
import { ImageSequenceProvider } from "./image-sequence";
import { MarkdownImage } from "./markdown-image";
import { LightboxProvider } from "./lightbox-provider";

const ID = "11111111-1111-4111-8111-111111111111";
const attachment: Attachment = {
  id: ID, workspace_id: "ws", issue_id: null, comment_id: null,
  chat_session_id: null, chat_message_id: null, uploader_type: "member", uploader_id: "u",
  markdown_url: `/api/attachments/${ID}/download`,
  filename: "shot.png", content_type: "image/png", size_bytes: 1,
  url: "https://cdn.example/图 v2.png", download_url: "https://cdn.example/signed.png?Signature=raw%2Bvalue",
  created_at: "2026-11-06T00:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  state.contexts.clear();
  state.setters.length = 0;
});

// Follow real parsing, provider values and press/open callbacks. Native leaves
// and hooks are stubbed; this does not render React Native or simulate swipes.
describe("mobile Markdown image sequence wiring", () => {
  it.each([
    ["Unicode", "![middle](https://cdn.example/图.png)", "https://cdn.example/%E5%9B%BE.png", []],
    ["angle URL with space", "![middle](<https://cdn.example/a b.png>)", "https://cdn.example/a%20b.png", []],
    ["single-segment relative space", "![middle](</chart v2.png>)", "/chart%20v2.png", []],
    ["escaped backslash", String.raw`![middle](<https://cdn.example/a\\(b).png>)`, "https://cdn.example/a%5C(b).png", []],
    ["existing percent escape", "![middle](https://cdn.example/a%20b.png)", "https://cdn.example/a%20b.png", []],
    ["numeric entity", "![middle](https://cdn.example/a.png?a=1&#x26;b=2)", "https://cdn.example/a.png?a=1&b=2", []],
    ["relative angle URL with space", "![middle](</uploads/a b.png>)", "/uploads/a%20b.png", []],
    ["legacy attachment URL", "![middle](<https://cdn.example/图 v2.png>)", attachment.download_url!, [attachment]],
    ["space entity", "![middle](https://cdn.example/a&#32;b.png)", "https://cdn.example/a%20b.png", []],
    ["named entity", "![middle](https://cdn.example/a.png?a=1&amp;b=2)", "https://cdn.example/a.png?a=1&b=2", []],
    ["escaped entity", String.raw`![middle](https://cdn.example/a.png?a=1\&amp;b=2)`, "https://cdn.example/a.png?a=1&amp;b=2", []],
    ["escaped nested entity", String.raw`![middle](https://cdn.example/a.png?a=1\&amp;amp;b=2)`, "https://cdn.example/a.png?a=1&amp;amp;b=2", []],
    ["escaped reference entity", String.raw`![middle][shot]

[shot]: https://cdn.example/a.png?a=1\&amp;amp;b=2`, "https://cdn.example/a.png?a=1&amp;amp;b=2", []],
    ["escaped collapsed reference entity", String.raw`![shot][]

[shot]: https://cdn.example/a.png?a=1\&amp;amp;b=2`, "https://cdn.example/a.png?a=1&amp;amp;b=2", []],
    ["escaped shortcut reference entity", String.raw`![shot]

[shot]: https://cdn.example/a.png?a=1\&amp;amp;b=2`, "https://cdn.example/a.png?a=1&amp;amp;b=2", []],
    ["escaped backslash before entity", String.raw`![middle](https://cdn.example/a.png?a=1\\&amp;b=2)`, "https://cdn.example/a.png?a=1%5C&b=2", []],
    ["entity-generated backslash", "![middle](https://cdn.example/a.png?a=1&bsol;&amp;b=2)", "https://cdn.example/a.png?a=1%5C&b=2", []],
    ["nested entity", "![middle](https://cdn.example/a.png?a=1&amp;amp;b=2)", "https://cdn.example/a.png?a=1&amp;b=2", []],
    ["reference", "![middle][shot]\n\n[shot]: https://cdn.example/图.png", "https://cdn.example/%E5%9B%BE.png", []],
    ["attachment", `![middle](/api/attachments/${ID}/download)`, attachment.download_url!, [attachment]],
  ] as const)("keeps the %s image inside the screen sequence", (_name, middle, expectedUri, attachments) => {
    const content = `![first](https://cdn.example/first.png)\n\n${middle}\n\n![last](https://cdn.example/last.png)`;
    const segments = splitMarkdown(preprocessMobileMarkdown(content)).filter((segment) => segment.type === "image");
    expect(segments).toHaveLength(3);
    const lightbox = LightboxProvider({ children: null });
    state.contexts.set(lightbox.type, lightbox.props.value);
    const provider = ImageSequenceProvider({ blocks: [{ content, attachments }], children: null });
    state.contexts.set(provider.type, provider.props.value);
    const image = MarkdownImage({ uri: segments[1]!.uri, attachments: [...attachments] });
    const renderedUri = image.props.children.props.children.props.source.uri;
    image.props.onPress();
    expect(state.setters[0]).toHaveBeenCalledExactlyOnceWith({
      images: [
        { uri: "https://cdn.example/first.png" },
        { uri: expectedUri },
        { uri: "https://cdn.example/last.png" },
      ],
      index: 1,
    });
    expect(renderedUri).toBe(expectedUri);
  });
});
