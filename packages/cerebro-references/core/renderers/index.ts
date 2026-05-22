// Side-effect import: registering a renderer here makes it available to
// every consumer of `getObjectRenderer`. Add new built-in renderers by
// creating a sibling file and importing it from this barrel.
import "./github-pr";

export { parseGithubPr, type GithubPrIdent } from "./github-pr";
