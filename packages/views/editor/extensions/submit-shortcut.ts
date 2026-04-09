import { Extension } from "@tiptap/core";

export function createSubmitExtension(onSubmit: () => void) {
  return Extension.create({
    name: "submitShortcut",
    addKeyboardShortcuts() {
      return {
        "Mod-Enter": () => {
          onSubmit();
          return true;
        },
      };
    },
  });
}
