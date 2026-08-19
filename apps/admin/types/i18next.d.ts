// apps/admin uses @multica/ui/components/ui/pagination directly, which calls
// the i18next v26 selector API (t($ => $.pagination_previous)). That selector
// form only typechecks when the `ui` namespace augmentation from
// packages/ui/types/i18next.ts is loaded into this app's compilation program.
//
// apps/web gets this for free transitively (via @multica/views' own
// resources-types.ts, pulled in by its use-t.ts side-effect import) because it
// depends on @multica/views. apps/admin has no reason to depend on the full
// views i18n resource graph (no translations of its own), so it pulls in just
// the `ui` slice directly — same mechanism packages/ui uses to typecheck this
// selector form standalone (see that file's own comment).
import "@multica/ui/i18n-types";
