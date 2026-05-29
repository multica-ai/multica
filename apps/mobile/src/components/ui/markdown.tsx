import { FileText } from "lucide-react-native";
import { Fragment, memo, type ReactNode, useEffect, useMemo, useState } from "react";
import { Linking, Pressable, StyleSheet, Text, View, Image, type ImageStyle, type TextStyle, type ViewStyle } from "react-native";
import {
  Renderer,
  useMarkdown,
  type MarkedStyles,
  type RendererInterface,
} from "react-native-marked";
import { MOBILE_ENV } from "../../runtime/env";
import { colors, radii, spacing } from "../../theme/tokens";
import {
  getIssueMentionId,
  isInlineFileLinkTitle,
  isMentionHref,
  isSafeHttpUrl,
  parseMobileFileCardHtml,
  preprocessMobileMarkdown,
  resolveMobileFileCardUrl,
} from "./markdown-utils";

const safeOpenUrl = (href: string) => {
  if (isSafeHttpUrl(href)) {
    void Linking.openURL(href);
  }
};

type MarkdownTextProps = {
  content: string;
  onIssueMentionPress?: (issueId: string) => void;
  onLinkPress?: (href: string) => boolean;
};

type MarkdownImageProps = {
  alt?: string;
  style?: ImageStyle;
  title?: string;
  uri: string;
};

function MarkdownImage({ alt, style, title, uri }: MarkdownImageProps): ReactNode {
  const [aspectRatio, setAspectRatio] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    Image.getSize(
      uri,
      (width, height) => {
        if (!cancelled && width > 0 && height > 0) {
          setAspectRatio(width / height);
        }
      },
      () => {
        if (!cancelled) {
          setAspectRatio(null);
        }
      },
    );

    return () => {
      cancelled = true;
    };
  }, [uri]);

  return (
    <Image
      accessibilityLabel={alt || title || "Image"}
      accessibilityRole="image"
      resizeMode="contain"
      source={{ uri }}
      style={[style, aspectRatio ? { aspectRatio, height: undefined } : styles.imageFallback]}
    />
  );
}

export const MarkdownText = memo(function MarkdownText({
  content,
  onIssueMentionPress,
  onLinkPress,
}: MarkdownTextProps) {
  const renderer = useMemo(
    () => new MulticaMarkdownRenderer(onIssueMentionPress, onLinkPress),
    [onIssueMentionPress, onLinkPress],
  );
  const value = useMemo(() => preprocessMobileMarkdown(content), [content]);
  const elements = useMarkdown(value, {
    renderer,
    styles: markdownStyles,
  });

  return (
    <View style={styles.root}>
      {elements.map((element, index) => (
        <Fragment key={index}>{element}</Fragment>
      ))}
    </View>
  );
});

class MulticaMarkdownRenderer extends Renderer implements RendererInterface {
  constructor(
    private readonly onIssueMentionPress?: (issueId: string) => void,
    private readonly onLinkPress?: (href: string) => boolean,
  ) {
    super();
  }

  link(
    children: string | ReactNode[],
    href: string,
    textStyle?: TextStyle,
    title?: string,
  ): ReactNode {
    const issueId = getIssueMentionId(href);
    if (issueId) {
      return (
        <Text
          accessibilityRole="link"
          key={this.getKey()}
          onPress={
            this.onIssueMentionPress ? () => this.onIssueMentionPress?.(issueId) : undefined
          }
          selectable
          style={[textStyle, styles.mention, styles.issueMention]}
        >
          {children}
        </Text>
      );
    }

    if (isMentionHref(href)) {
      return (
        <Text key={this.getKey()} selectable style={[textStyle, styles.mention]}>
          {children}
        </Text>
      );
    }

    if (isInlineFileLinkTitle(title)) {
      const resolvedUrl = resolveMobileFileCardUrl(href, MOBILE_ENV.apiBaseUrl);
      return (
        <Text
          accessibilityHint={resolvedUrl ? "Opens in a browser" : undefined}
          accessibilityLabel="File link"
          accessibilityRole={resolvedUrl ? "link" : undefined}
          key={this.getKey()}
          onPress={resolvedUrl ? () => safeOpenUrl(resolvedUrl) : undefined}
          selectable
          style={[textStyle, styles.inlineFileLink]}
        >
          {children}
        </Text>
      );
    }

    return (
      <Text
        accessibilityHint={isSafeHttpUrl(href) ? "Opens in a browser" : undefined}
        accessibilityLabel={title || "Link"}
        accessibilityRole={isSafeHttpUrl(href) ? "link" : undefined}
        key={this.getKey()}
        onPress={isSafeHttpUrl(href) ? () => {
          if (this.onLinkPress?.(href)) return;
          safeOpenUrl(href);
        } : undefined}
        selectable
        style={textStyle}
      >
        {children}
      </Text>
    );
  }

  code(
    text: string,
    _language?: string,
    containerStyle?: ViewStyle,
    _textStyle?: TextStyle,
  ): ReactNode {
    return (
      <View key={this.getKey()} style={containerStyle}>
        <Text selectable style={styles.codeBlockText}>
          {text}
        </Text>
      </View>
    );
  }

  html(raw: string | ReactNode[], textStyle?: TextStyle): ReactNode {
    if (typeof raw !== "string") {
      return (
        <Text key={this.getKey()} selectable style={textStyle}>
          {raw}
        </Text>
      );
    }

    const card = parseMobileFileCardHtml(raw);
    if (!card) {
      return (
        <Text key={this.getKey()} selectable style={textStyle}>
          {raw}
        </Text>
      );
    }

    const resolvedUrl = resolveMobileFileCardUrl(card.href, MOBILE_ENV.apiBaseUrl);
    const disabled = !resolvedUrl;

    return (
      <Pressable
        accessibilityRole={disabled ? undefined : "button"}
        disabled={disabled}
        key={this.getKey()}
        onPress={resolvedUrl ? () => safeOpenUrl(resolvedUrl) : undefined}
        style={({ pressed }) => [
          styles.fileCard,
          pressed && !disabled && styles.fileCardPressed,
        ]}
      >
        <View style={styles.fileIcon}>
          <FileText color={colors.info} size={18} strokeWidth={2} />
        </View>
        <Text numberOfLines={2} style={styles.fileName}>
          {card.filename}
        </Text>
      </Pressable>
    );
  }

  image(uri: string, alt?: string, style?: ImageStyle, title?: string): ReactNode {
    if (!isSafeHttpUrl(uri) && !uri.startsWith("/uploads/")) {
      return (
        <Text key={this.getKey()} selectable style={styles.text}>
          {alt || title || uri}
        </Text>
      );
    }

    const imageUri = uri.startsWith("/uploads/")
      ? `${MOBILE_ENV.apiBaseUrl.replace(/\/$/, "")}${uri}`
      : uri;

    return (
      <MarkdownImage
        alt={alt}
        key={this.getKey()}
        style={style}
        title={title}
        uri={imageUri}
      />
    );
  }
}

const markdownStyles: MarkedStyles = {
  text: {
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 21,
  },
  paragraph: {
    marginVertical: 1,
  },
  h1: {
    color: colors.foreground,
    fontSize: 20,
    fontWeight: "700",
    lineHeight: 28,
    marginTop: spacing.xs,
  },
  h2: {
    color: colors.foreground,
    fontSize: 17,
    fontWeight: "700",
    lineHeight: 24,
    marginTop: spacing.xs,
  },
  h3: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "700",
    lineHeight: 22,
    marginTop: spacing.xs,
  },
  h4: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "700",
    lineHeight: 22,
    marginTop: spacing.xs,
  },
  h5: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "700",
    lineHeight: 21,
    marginTop: spacing.xs,
  },
  h6: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "700",
    lineHeight: 21,
    marginTop: spacing.xs,
  },
  strong: {
    fontWeight: "700",
  },
  em: {
    fontStyle: "italic",
  },
  strikethrough: {
    textDecorationLine: "line-through",
  },
  link: {
    color: colors.info,
    fontSize: 14,
    lineHeight: 21,
    textDecorationLine: "underline",
  },
  codespan: {
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    color: colors.foreground,
    fontFamily: "Courier",
    fontSize: 13,
    paddingHorizontal: 3,
  },
  code: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    padding: spacing.md,
  },
  blockquote: {
    backgroundColor: colors.muted,
    borderLeftColor: colors.border,
    borderLeftWidth: 3,
    borderRadius: radii.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  list: {
    gap: spacing.sm,
    paddingRight: spacing.xs,
  },
  li: {
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 21,
  },
  hr: {
    backgroundColor: colors.border,
    height: StyleSheet.hairlineWidth,
  },
  image: {
    borderRadius: radii.md,
    width: "100%",
  },
  table: {
    borderColor: colors.border,
    borderWidth: StyleSheet.hairlineWidth,
  },
  tableRow: {
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  tableCell: {
    padding: spacing.sm,
  },
};

const styles = StyleSheet.create({
  root: {
    gap: spacing.md,
  },
  text: {
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 21,
  },
  imageFallback: {
    height: 180,
  },
  mention: {
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    color: colors.info,
    fontWeight: "600",
  },
  issueMention: {
    textDecorationLine: "underline",
  },
  inlineFileLink: {
    color: colors.info,
    fontSize: 14,
    fontWeight: "600",
    lineHeight: 21,
    textDecorationLine: "underline",
  },
  codeBlockText: {
    color: colors.foreground,
    fontFamily: "Courier",
    fontSize: 13,
    lineHeight: 18,
  },
  fileCard: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  fileCardPressed: {
    backgroundColor: colors.muted,
  },
  fileIcon: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    height: 32,
    justifyContent: "center",
    width: 32,
  },
  fileName: {
    color: colors.foreground,
    flex: 1,
    fontSize: 14,
    fontWeight: "600",
    lineHeight: 19,
  },
});
