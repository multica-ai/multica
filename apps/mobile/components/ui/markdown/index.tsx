import { Fragment } from "react";
import { useColorScheme, View } from "react-native";
import { useMarkdown } from "react-native-marked";

import { MulticaRenderer } from "./renderer";
import { MARKDOWN_STYLES, type Variant } from "./theme";

// Mobile markdown renderer.
//
// Architecture: react-native-marked's `useMarkdown` hook handles parsing
// (marked.js + GFM) and traversal; MulticaRenderer overrides only the three
// nodes that need multica-specific behavior (mention chip, expo-image, fenced
// code block). Styling is driven by MARKDOWN_STYLES in theme.ts.
//
// We use the hook (not the <Markdown> component) so output is plain
// ReactNode[] without an internal FlatList — both consumers
// (IssueDetailView's ScrollView and CommentList's FlashList) provide their
// own scroll container, and nesting a virtualized list inside another is an
// RN antipattern.

export interface MarkdownProps {
  content: string;
  variant?: Variant;
}

// Renderer is stateless across renders (it only generates React keys via
// internal slugger, which is reset per parse by the hook). Single instance
// avoids reallocating on every Markdown mount in long comment lists.
const renderer = new MulticaRenderer();

export function Markdown({ content, variant = "default" }: MarkdownProps) {
  const colorScheme = useColorScheme();
  const elements = useMarkdown(content ?? "", {
    colorScheme,
    renderer,
    styles: MARKDOWN_STYLES[variant],
  });

  if (!content) return null;

  return (
    <View>
      {elements.map((el, i) => (
        <Fragment key={i}>{el}</Fragment>
      ))}
    </View>
  );
}
