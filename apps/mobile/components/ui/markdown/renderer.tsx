import { type ReactNode } from "react";
import {
  ScrollView,
  Text,
  View,
  type ImageStyle,
  type TextStyle,
  type ViewStyle,
} from "react-native";
import { Renderer, type RendererInterface } from "react-native-marked";

import { COLOR, FONT_MONO } from "./theme";
import { MarkdownImage } from "./markdown-image";
import { MentionChip } from "./mention-chip";

const MENTION_REGEX =
  /^mention:\/\/(member|agent|issue|all)\/([0-9a-fA-F-]+|all)$/;

// Custom renderer for multica's mobile markdown. Three overrides:
//
// - link():  detect `mention://type/id` and render a MentionChip; everything
//   else falls through to the parent class (selectable Text + Linking).
// - image(): use expo-image with intrinsic-aspect detection (MarkdownImage)
//   instead of the library's RN Image fallback.
// - code():  render the fenced block ourselves so we control the monospace
//   font + muted background. The library's Parser passes `styles.em` as the
//   textStyle for block code (a quirk of its style mapping), so without this
//   override fenced code would render in italic body text. Inline `code` is
//   left to the base renderer + `styles.codespan`.
//
// Everything else (paragraph, heading, list, table, blockquote, br, hr,
// strong/em/del, codespan) uses the base Renderer; styling is driven by
// MarkedStyles in theme.ts.
export class MulticaRenderer extends Renderer implements RendererInterface {
  link(
    children: string | ReactNode[],
    href: string,
    styles?: TextStyle,
    title?: string,
  ): ReactNode {
    const m = MENTION_REGEX.exec(href);
    if (m) {
      const [, type] = m;
      const label = extractLabel(children) || href;
      return <MentionChip key={this.getKey()} type={type} label={label} />;
    }
    return super.link(children, href, styles, title);
  }

  image(
    uri: string,
    alt?: string,
    _style?: ImageStyle,
    _title?: string,
  ): ReactNode {
    return <MarkdownImage key={this.getKey()} uri={uri} alt={alt} />;
  }

  code(
    text: string,
    _language?: string,
    containerStyle?: ViewStyle,
  ): ReactNode {
    return (
      <View
        key={this.getKey()}
        style={[
          {
            backgroundColor: COLOR.muted,
            borderRadius: 8,
            marginVertical: 8,
          },
          containerStyle,
        ]}
      >
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          contentContainerStyle={{ padding: 12 }}
        >
          <Text
            selectable
            style={{
              fontFamily: FONT_MONO,
              fontSize: 13,
              color: COLOR.foreground,
              lineHeight: 20,
            }}
          >
            {text}
          </Text>
        </ScrollView>
      </View>
    );
  }
}

function extractLabel(children: string | ReactNode[]): string {
  if (typeof children === "string") return children;
  // For mention markdown `[Plain Label](mention://...)`, the parser passes
  // children as a single-element ReactNode[]. Walk one level for safety.
  return children
    .map((n) => {
      if (typeof n === "string") return n;
      if (typeof n === "number") return String(n);
      return "";
    })
    .join("");
}
