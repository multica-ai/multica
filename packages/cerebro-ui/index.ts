export { JumpToLatestButton } from "./components/jump-to-latest-button";
export { IssueContextTrigger } from "./components/issue-context-trigger";
export { ComposerExpandToggle } from "./components/composer-expand-toggle";
export {
  AttachmentChip,
  attachmentDocType,
  DOC_TYPE_STYLES,
  ATTACHMENT_CHIP_WIDTH,
  ATTACHMENT_CHIP_HEIGHT,
  type AttachmentChipProps,
  type AttachmentDocType,
  type DocTypeStyle,
} from "./components/attachment-chip";
export { ZoomableImage, type ZoomableImageProps } from "./components/zoomable-image";
export {
  ImageGallery,
  type ImageGalleryProps,
  type GalleryImage,
} from "./components/image-gallery";
export { useZoomPan, type ZoomPanOptions, type ZoomPanState } from "./hooks/use-zoom-pan";
export { useStickyBottom } from "./hooks/use-sticky-bottom";
export { useHighlightCommentScroll } from "./hooks/use-highlight-comment-scroll";
export { useNavScrollState } from "./hooks/use-nav-scroll-state";
export { useComposerHeight, type ComposerHeight } from "./hooks/use-composer-height";
export { EditorFormattingToolbar } from "./components/editor-formatting-toolbar";
export { EditorToolbarSettings } from "./components/editor-toolbar-settings";
export { AccessDiagnostics } from "./components/access-diagnostics";
export { TaskAccessDisclosure } from "./components/task-access-disclosure";
export {
  DEFAULT_EDITOR_TOOLBAR_ORDER,
  EDITOR_TOOLBAR_ORDER_KEY,
  readEditorToolbarOrder,
  type EditorToolbarActionId,
} from "./components/editor-toolbar-preferences";
