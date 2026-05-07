// Side-effect import: pulls the i18next module augmentation into the
// compilation graph. Without this, apps that consume @multica/views won't
// see the resources types or the selector-API enablement, and their
// typecheck would reject `t($ => $.foo.bar)` calls inside views.
import "./resources-types";

// Project alias for react-i18next's useTranslation hook.
// Use the selector form when calling: t($ => $.signin.title)
export { useTranslation as useT } from "react-i18next";
