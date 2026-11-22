import { ReactNodeViewRenderer } from "@tiptap/react";
import { SlashCommandView } from "./slash-command-view";
import { SlashCommandSchema } from "../schema/slash-command";

export const SlashCommandExtension = SlashCommandSchema.extend({
  addNodeView() {
    return ReactNodeViewRenderer(SlashCommandView);
  },
});
