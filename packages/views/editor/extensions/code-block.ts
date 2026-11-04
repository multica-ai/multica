import CodeBlockLowlight from "@tiptap/extension-code-block-lowlight";

export const CodeBlockExtension = CodeBlockLowlight.extend({
  renderMarkdown(node, helpers) {
    const content = helpers.renderChildren(node.content ?? []);
    // A literal fence inside the code must not close the serialized block.
    let fenceLength = 3;
    for (const [run] of content.matchAll(/`+/g)) {
      fenceLength = Math.max(fenceLength, run.length + 1);
    }
    const fence = "`".repeat(fenceLength);
    return `${fence}${node.attrs?.language ?? ""}\n${content}\n${fence}`;
  },
});
