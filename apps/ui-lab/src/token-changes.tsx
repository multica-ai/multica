import { useTranslation } from "react-i18next";
import { Check, RotateCcw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  baseline,
  changeCount,
  colorTokens,
  sizeTokens,
  easingTokens,
  updateToken,
  type Draft,
  type Scope,
} from "./tokens";

export function TokenChanges({
  draft,
  onEdit,
}: {
  draft: Draft;
  onEdit: (draft: Draft) => void;
}) {
  const { t } = useTranslation("uiLab");
  const count = changeCount(draft);
  const reset = (scope: Scope, key: string) =>
    onEdit(updateToken(draft, scope, key, baseline[scope][key]!));
  const changes = (
    Object.entries(draft) as [Scope, Record<string, string>][]
  ).flatMap(([scope, values]) =>
    Object.entries(values).map(([key, value]) => ({ scope, key, value })),
  );
  return (
    <>
      <div className="property-changes-intro">
        <strong>
          {count
            ? t(($) => $.lab.preview.changeCount, { count })
            : t(($) => $.inspector.changes.unchanged)}
        </strong>
      </div>
      {changes.map(({ scope, key, value }) => {
        const label =
          sizeTokens.find((token) => token.key === key)?.label ??
          easingTokens.find((token) => token.key === key)?.label ??
          colorTokens.find(([token]) => token === key)?.[1];
        return (
          <div className="property-change" key={`${scope}:${key}`}>
            <header>
              <strong>{label ? t(($) => $.tokens.labels[label]) : key}</strong>
              <span>
                {scope === "shared"
                  ? t(($) => $.inspector.scope.shared)
                  : scope === "light"
                    ? t(($) => $.lab.theme.light)
                    : t(($) => $.lab.theme.dark)}
              </span>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={t(($) => $.inspector.actions.restore, {
                  key,
                  scope:
                    scope === "shared"
                      ? t(($) => $.inspector.scope.shared)
                      : t(($) => $.lab.theme[scope]),
                })}
                onClick={() => reset(scope, key)}
              >
                <RotateCcw />
              </Button>
            </header>
            <code>{key}</code>
            <div className="property-change-values">
              <del>{baseline[scope][key]}</del>
              <span>{value}</span>
            </div>
          </div>
        );
      })}
      {!count && (
        <div className="property-empty">
          <Check className="size-6" />
          <span>{t(($) => $.inspector.changes.empty)}</span>
        </div>
      )}
    </>
  );
}
