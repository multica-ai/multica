export {
  parsePatch,
  unquotePath,
  type DiffHunk,
  type DiffLine,
  type DiffLineKind,
  type ParsedDiffFile,
} from "./parse-patch";
export {
  buildDiffFiles,
  diffTotals,
  groupFilesByDirectory,
  latestLineOfWork,
  pullRequestForChange,
  runNumber,
  splitHunkRows,
  type DiffFileGroup,
  type DiffFileView,
  type DiffTotals,
  type SplitRow,
} from "./diff-model";
