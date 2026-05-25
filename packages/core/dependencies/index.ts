// CEREBRO-PATCH(issue-dependencies): public surface for the dependencies module.
export { dependencyKeys, issueDependenciesOptions } from "./queries";
export {
  useAddBlocks,
  useAddBlockedBy,
  useAddRelated,
  useRemoveBlocks,
  useRemoveBlockedBy,
  useRemoveRelated,
} from "./mutations";
