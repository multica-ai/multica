import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { BookOpen, Terminal, Rocket, Code } from "lucide-react";

export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <span className="font-semibold text-base">Multica Docs</span>
    ),
  },
  links: [
    {
      text: "Documentation",
      url: "/docs",
      active: "nested-url",
    },
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
