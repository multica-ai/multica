import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";

export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <span className="font-semibold text-base">Multica Docs</span>
    ),
  },
  links: [
    {
      text: "GitHub",
      url: "https://github.com/multica-ai/multica",
    },
    {
      text: "Cloud",
      url: "https://multica.ai",
    },
  ],
};
