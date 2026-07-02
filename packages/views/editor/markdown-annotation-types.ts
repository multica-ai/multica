export interface SourcePoint {
  line: number;
  character: number;
  offset: number;
}

export interface SourceRange {
  start: SourcePoint;
  end: SourcePoint;
}

export interface MarkdownAnnotationDraft {
  id: string;
  attachmentId: string;
  filename: string;
  range: SourceRange;
  quote: string;
  note: string;
  createdAt: number;
}

export interface MarkdownSourceSelection {
  range: SourceRange;
  quote: string;
}
