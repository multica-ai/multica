import { ReactNodeViewRenderer } from "@tiptap/react";
import { MentionView } from "./mention-view";
import { MentionSchema } from "../schema/mention";

export { mentionText } from "../schema/mention";

export const BaseMentionExtension = MentionSchema.extend({
  addNodeView() {
    return ReactNodeViewRenderer(MentionView);
  },
});
