import { FileText, RectangleHorizontal, RectangleVertical } from "lucide-react-native";
import { Fragment, isValidElement, memo, type ReactNode, useEffect, useMemo, useState } from "react";
import {
  Image,
  Linking,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
  type GestureResponderEvent,
  type ImageStyle,
  type LayoutChangeEvent,
  type TextStyle,
  type ViewStyle,
} from "react-native";
import {
  Renderer,
  useMarkdown,
  type MarkedStyles,
  type RendererInterface,
} from "react-native-marked";
import { MOBILE_ENV } from "../../runtime/env";
import { colors, radii, spacing } from "../../theme/tokens";
import { ImagePreviewModal, type PreviewImageItem } from "./image-preview-modal";
import { ScreenTitleBar } from "./screen-title-bar";
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
  onPress?: (event: GestureResponderEvent) => void;
  style?: ImageStyle;
  title?: string;
  uri: string;
};

type TablePreviewOrientation = "landscape" | "portrait";
const TABLE_PREVIEW_MIN_COLUMN_WIDTH = 56;
const TABLE_PREVIEW_DEFAULT_COLUMN_WIDTH = 104;
const TABLE_PREVIEW_PORTRAIT_MAX_COLUMN_WIDTH = 320;
const TABLE_PREVIEW_LANDSCAPE_MAX_COLUMN_WIDTH = 420;

function MarkdownImage({ alt, onPress, style, title, uri }: MarkdownImageProps): ReactNode {
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
    <Pressable
      accessibilityLabel={alt || title || "Image"}
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => pressed && styles.mediaPressed}
    >
      <Image
        accessibilityIgnoresInvertColors
        resizeMode="contain"
        source={{ uri }}
        style={[style, aspectRatio ? { aspectRatio, height: undefined } : styles.imageFallback]}
      />
    </Pressable>
  );
}

export const MarkdownText = memo(function MarkdownText({
  content,
  onIssueMentionPress,
  onLinkPress,
}: MarkdownTextProps) {
  const [previewImage, setPreviewImage] = useState<PreviewImageItem | null>(null);
  const renderer = useMemo(
    () => new MulticaMarkdownRenderer(onIssueMentionPress, onLinkPress, setPreviewImage),
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
      <ImagePreviewModal
        images={previewImage ? [previewImage] : []}
        initialIndex={0}
        onClose={() => setPreviewImage(null)}
        open={Boolean(previewImage)}
      />
    </View>
  );
});

class MulticaMarkdownRenderer extends Renderer implements RendererInterface {
  constructor(
    private readonly onIssueMentionPress?: (issueId: string) => void,
    private readonly onLinkPress?: (href: string) => boolean,
    private readonly onImagePress?: (image: PreviewImageItem) => void,
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
    const imageUri = resolveMarkdownImageUri(uri);
    if (!imageUri) {
      return (
        <Text key={this.getKey()} selectable style={styles.text}>
          {alt || title || uri}
        </Text>
      );
    }

    const image = createPreviewImageItem(imageUri, alt || title);

    return (
      <MarkdownImage
        alt={alt}
        key={this.getKey()}
        onPress={(event) => {
          event.stopPropagation();
          this.onImagePress?.(image);
        }}
        style={style}
        title={title}
        uri={imageUri}
      />
    );
  }

  table(
    header: ReactNode[][],
    rows: ReactNode[][][],
    tableStyle?: ViewStyle,
    rowStyle?: ViewStyle,
    cellStyle?: ViewStyle,
  ): ReactNode {
    return (
      <MarkdownTable
        cellStyle={cellStyle}
        header={header}
        key={this.getKey()}
        rows={rows}
        rowStyle={rowStyle}
        tableStyle={tableStyle}
      />
    );
  }
}

function MarkdownTable({
  cellStyle,
  header,
  rows,
  rowStyle,
  tableStyle,
}: {
  cellStyle?: ViewStyle;
  header: ReactNode[][];
  rows: ReactNode[][][];
  rowStyle?: ViewStyle;
  tableStyle?: ViewStyle;
}) {
  const [open, setOpen] = useState(false);
  const [previewOrientation, setPreviewOrientation] = useState<TablePreviewOrientation>("landscape");
  const [previewBodySize, setPreviewBodySize] = useState({ height: 0, width: 0 });
  const { height, width } = useWindowDimensions();
  const columnCount = Math.max(header.length, ...rows.map((row) => row.length), 1);
  const previewLandscape = previewOrientation === "landscape";
  const landscapeContentWidth = previewBodySize.height || Math.max(width, height);
  const portraitContentWidth = previewBodySize.width || width;
  const previewViewportWidth = Math.max(
    280,
    (previewLandscape ? landscapeContentWidth : portraitContentWidth) - spacing.lg * 2,
  );
  const visibleColumnCount = previewLandscape ? 4 : 2.4;
  const fallbackPreviewCellWidth = Math.max(
    previewLandscape ? 120 : 96,
    Math.min(previewLandscape ? 220 : 180, Math.floor(previewViewportWidth / visibleColumnCount)),
  );
  const previewCellWidths = useMemo(
    () => getTableColumnWidths({
      columnCount,
      fallbackWidth: fallbackPreviewCellWidth,
      header,
      maxWidth: previewLandscape
        ? TABLE_PREVIEW_LANDSCAPE_MAX_COLUMN_WIDTH
        : TABLE_PREVIEW_PORTRAIT_MAX_COLUMN_WIDTH,
      rows,
    }),
    [columnCount, fallbackPreviewCellWidth, header, previewLandscape, rows],
  );
  const previewTableWidth = previewCellWidths.reduce((total, next) => total + next, 0);
  const nextOrientation = previewLandscape ? "portrait" : "landscape";

  function openPreview(event: GestureResponderEvent) {
    event.stopPropagation();
    setPreviewOrientation("landscape");
    setOpen(true);
  }

  function handlePreviewBodyLayout(event: LayoutChangeEvent) {
    const { height: nextHeight, width: nextWidth } = event.nativeEvent.layout;
    setPreviewBodySize((size) => {
      if (Math.round(size.height) === Math.round(nextHeight) && Math.round(size.width) === Math.round(nextWidth)) {
        return size;
      }
      return { height: nextHeight, width: nextWidth };
    });
  }

  return (
    <>
      <Pressable
        accessibilityRole="button"
        onPress={openPreview}
        style={({ pressed }) => [
          styles.table,
          tableStyle,
          pressed && styles.mediaPressed,
        ]}
      >
        <MarkdownTableRows
          cellStyle={cellStyle}
          columnCount={columnCount}
          header={header}
          rows={rows}
          rowStyle={rowStyle}
        />
      </Pressable>
      <Modal animationType="slide" onRequestClose={() => setOpen(false)} visible={open}>
        <View style={styles.tableModal}>
          <ScreenTitleBar
            onBack={() => setOpen(false)}
            right={
              <Pressable
                accessibilityLabel={`Switch to ${nextOrientation}`}
                accessibilityRole="button"
                hitSlop={8}
                onPress={() => setPreviewOrientation(nextOrientation)}
                style={({ pressed }) => [
                  styles.tableOrientationButton,
                  pressed && styles.mediaPressed,
                ]}
              >
                {previewLandscape ? (
                  <RectangleVertical color={colors.foreground} size={22} strokeWidth={2.1} />
                ) : (
                  <RectangleHorizontal color={colors.foreground} size={22} strokeWidth={2.1} />
                )}
              </Pressable>
            }
            title="Table"
          />
          <View onLayout={handlePreviewBodyLayout} style={styles.tableModalBody}>
            {previewLandscape ? (
              <View style={styles.tableLandscapeStage}>
                <View
                  style={[
                    styles.tableLandscapeContent,
                    previewBodySize.height > 0 && previewBodySize.width > 0
                      ? {
                        height: previewBodySize.width,
                        width: previewBodySize.height,
                      }
                      : null,
                  ]}
                >
                  <MarkdownTablePreview
                    cellStyle={cellStyle}
                    columnCount={columnCount}
                    header={header}
                    previewCellWidths={previewCellWidths}
                    previewTableWidth={previewTableWidth}
                    rows={rows}
                    rowStyle={rowStyle}
                    tableStyle={tableStyle}
                  />
                </View>
              </View>
            ) : (
              <MarkdownTablePreview
                cellStyle={cellStyle}
                columnCount={columnCount}
                header={header}
                previewCellWidths={previewCellWidths}
                previewTableWidth={previewTableWidth}
                rows={rows}
                rowStyle={rowStyle}
                tableStyle={tableStyle}
              />
            )}
          </View>
        </View>
      </Modal>
    </>
  );
}

function MarkdownTablePreview({
  cellStyle,
  columnCount,
  header,
  previewCellWidths,
  previewTableWidth,
  rows,
  rowStyle,
  tableStyle,
}: {
  cellStyle?: ViewStyle;
  columnCount: number;
  header: ReactNode[][];
  previewCellWidths: number[];
  previewTableWidth: number;
  rows: ReactNode[][][];
  rowStyle?: ViewStyle;
  tableStyle?: ViewStyle;
}) {
  const hasHeader = header.length > 0;

  return (
    <View style={styles.tableModalContent}>
      <ScrollView
        contentContainerStyle={styles.tableModalHorizontalContent}
        horizontal
        style={styles.tableModalHorizontalScroll}
      >
        <View style={[styles.table, styles.tablePreviewTable, { width: previewTableWidth }, tableStyle]}>
          {hasHeader ? (
            <MarkdownTableRows
              cellStyle={cellStyle}
              columnCount={columnCount}
              header={header}
              previewCellWidths={previewCellWidths}
              rows={[]}
              rowStyle={rowStyle}
            />
          ) : null}
          <ScrollView style={styles.tablePreviewBodyScroll}>
            <MarkdownTableRows
              cellStyle={cellStyle}
              columnCount={columnCount}
              header={[]}
              previewCellWidths={previewCellWidths}
              rows={rows}
              rowStyle={rowStyle}
            />
          </ScrollView>
        </View>
      </ScrollView>
    </View>
  );
}

function MarkdownTableRows({
  cellStyle,
  columnCount,
  header,
  previewCellWidths,
  rows,
  rowStyle,
}: {
  cellStyle?: ViewStyle;
  columnCount: number;
  header: ReactNode[][];
  previewCellWidths?: number[];
  rows: ReactNode[][][];
  rowStyle?: ViewStyle;
}) {
  return (
    <>
      {header.length > 0 ? (
        <View style={[styles.tableRow, styles.tableHeaderRow, rowStyle]}>
          {Array.from({ length: columnCount }, (_, index) => (
            <View
              key={index}
              style={[
                styles.tableCell,
                previewCellWidths ? { width: previewCellWidths[index] ?? TABLE_PREVIEW_DEFAULT_COLUMN_WIDTH } : { flex: 1 },
                index === columnCount - 1 && styles.tableLastCell,
                cellStyle,
              ]}
            >
              {header[index] ?? null}
            </View>
          ))}
        </View>
      ) : null}
      {rows.map((row, rowIndex) => (
        <View key={rowIndex} style={[styles.tableRow, rowStyle]}>
          {Array.from({ length: columnCount }, (_, cellIndex) => (
            <View
              key={cellIndex}
              style={[
                styles.tableCell,
                previewCellWidths ? { width: previewCellWidths[cellIndex] ?? TABLE_PREVIEW_DEFAULT_COLUMN_WIDTH } : { flex: 1 },
                cellIndex === columnCount - 1 && styles.tableLastCell,
                cellStyle,
              ]}
            >
              {row[cellIndex] ?? null}
            </View>
          ))}
        </View>
      ))}
    </>
  );
}

function getTableColumnWidths({
  columnCount,
  fallbackWidth,
  header,
  maxWidth,
  rows,
}: {
  columnCount: number;
  fallbackWidth: number;
  header: ReactNode[][];
  maxWidth: number;
  rows: ReactNode[][][];
}): number[] {
  return Array.from({ length: columnCount }, (_, columnIndex) => {
    const cells = [
      header[columnIndex],
      ...rows.map((row) => row[columnIndex]),
    ];
    const maxTextUnits = cells.reduce((max, cell) => {
      const text = extractNodeText(cell).trim();
      if (!text) return max;
      const lineMax = text
        .split(/\r?\n/)
        .reduce((lineMaxValue, line) => Math.max(lineMaxValue, estimateTextUnits(line)), 0);
      return Math.max(max, lineMax);
    }, 0);

    if (maxTextUnits <= 0) {
      return Math.max(TABLE_PREVIEW_MIN_COLUMN_WIDTH, Math.min(fallbackWidth, TABLE_PREVIEW_DEFAULT_COLUMN_WIDTH));
    }

    const estimatedWidth = Math.ceil(maxTextUnits * 7.2 + spacing.sm * 2 + 12);
    return Math.max(TABLE_PREVIEW_MIN_COLUMN_WIDTH, Math.min(maxWidth, estimatedWidth));
  });
}

function extractNodeText(node: ReactNode): string {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(extractNodeText).join("");
  if (isValidElement(node)) {
    const props = node.props as { children?: ReactNode };
    return extractNodeText(props.children);
  }
  return "";
}

function estimateTextUnits(text: string): number {
  let units = 0;
  for (const char of text) {
    if (/\s/.test(char)) {
      units += 0.5;
    } else if (/[\u1100-\u11ff\u2e80-\u9fff\uf900-\ufaff\uff00-\uffef]/.test(char)) {
      units += 2;
    } else {
      units += 1;
    }
  }
  return units;
}

function resolveMarkdownImageUri(uri: string): string | null {
  if (isSafeHttpUrl(uri)) return uri;
  if (!uri.startsWith("/uploads/")) return null;
  return `${MOBILE_ENV.apiBaseUrl.replace(/\/$/, "")}${uri}`;
}

function createPreviewImageItem(uri: string, label?: string): PreviewImageItem {
  return {
    contentType: getImageContentType(uri),
    filename: label || getFilenameFromUri(uri) || "image",
    id: `markdown-image-${hashString(uri)}`,
    uri,
  };
}

function getFilenameFromUri(uri: string): string | undefined {
  const path = uri.split(/[?#]/)[0] ?? "";
  const filename = path.split("/").pop();
  if (!filename) return undefined;

  try {
    return decodeURIComponent(filename);
  } catch {
    return filename;
  }
}

function getImageContentType(uri: string): string | undefined {
  const extension = getFilenameFromUri(uri)?.match(/\.([a-z0-9]+)$/i)?.[1]?.toLowerCase();
  if (!extension) return undefined;
  if (extension === "jpg") return "image/jpeg";
  return `image/${extension}`;
}

function hashString(value: string): string {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) | 0;
  }
  return Math.abs(hash).toString(36);
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
  mediaPressed: {
    opacity: 0.72,
  },
  table: {
    alignSelf: "stretch",
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  tableCell: {
    borderRightColor: colors.border,
    borderRightWidth: StyleSheet.hairlineWidth,
    minWidth: 0,
    padding: spacing.sm,
  },
  tableHeaderRow: {
    backgroundColor: colors.muted,
  },
  tableLastCell: {
    borderRightWidth: 0,
  },
  tableModal: {
    backgroundColor: colors.background,
    flex: 1,
  },
  tableModalBody: {
    flex: 1,
  },
  tableModalContent: {
    flex: 1,
    padding: spacing.lg,
  },
  tableModalHorizontalContent: {
    flexGrow: 1,
  },
  tableModalHorizontalScroll: {
    flex: 1,
  },
  tableLandscapeContent: {
    transform: [{ rotate: "90deg" }],
  },
  tableLandscapeStage: {
    alignItems: "center",
    flex: 1,
    justifyContent: "center",
    overflow: "hidden",
  },
  tableOrientationButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 40,
    justifyContent: "center",
    width: 40,
  },
  tablePreviewBodyScroll: {
    flex: 1,
  },
  tablePreviewTable: {
    height: "100%",
  },
  tableRow: {
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
  },
});
