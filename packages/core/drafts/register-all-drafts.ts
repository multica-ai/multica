/**
 * Side-effect module that imports every module-level draft store so their
 * `registerDraftCleanup` calls have run before any cleanup path executes.
 *
 * Self-registration (see cleanup-registry) only knows about stores that have
 * been imported. A cleanup caller that imports only its own module would
 * otherwise see an empty registry and skip persisted draft keys whose store
 * happened not to be loaded yet — a narrower version of the very leak this
 * system exists to fix. Importing this module from `storage-cleanup` guarantees
 * the registry is complete wherever cleanup runs.
 *
 * Chat is intentionally absent: its keys are registered inside `createChatStore`
 * (the store is instance-scoped, created by the chat provider), so its keys
 * only exist once that instance does, and registration is tied to the same
 * lifecycle. There is nothing to import here that would register them earlier.
 */
import "../issues/stores/draft-store";
import "../issues/stores/quick-create-store";
import "../issues/stores/comment-draft-store";
import "../projects/draft-store";
import "../feedback/draft-store";
