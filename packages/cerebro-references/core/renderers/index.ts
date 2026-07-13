// Side-effect import: registering a renderer here makes it available to
// every consumer of `getObjectRenderer`. Add new built-in renderers by
// creating a sibling file and importing it from this barrel.
import "./github-pr";
import "./note-comment";

export { parseGithubPr, type GithubPrIdent } from "./github-pr";
export {
  formatNoteCommentTitle,
  resolveNoteCommentUrl,
} from "./note-comment";
