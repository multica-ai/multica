import { useTranslation } from "react-i18next";
import { Markdown } from "@multica/ui/markdown";
import { Button } from "@multica/ui/components/ui/button";
import buttonEn from "@multica/ui/docs/button.md?raw";
import buttonZh from "@multica/ui/docs/button.zh.md?raw";
import dialogEn from "@multica/ui/docs/dialog.md?raw";
import dialogZh from "@multica/ui/docs/dialog.zh.md?raw";

const rules = {
  button: { en: buttonEn, zh: buttonZh },
  dialog: { en: dialogEn, zh: dialogZh },
};

export function ComponentRules({
  component,
}: {
  component: keyof typeof rules;
}) {
  const { t, i18n } = useTranslation("uiLab");
  const locale = i18n.language === "zh-Hans" ? "zh" : "en";
  const content = rules[component][locale];
  return (
    <details className="component-rules">
      <summary>{t(($) => $.rules.content.title)}</summary>
      <div className="rules-source">
        <code>
          packages/ui/docs/{component}
          {locale === "zh" ? ".zh" : ""}.md
        </code>
        <Button
          variant="outline"
          size="sm"
          render={
            <a
              href={`data:text/markdown;charset=utf-8,${encodeURIComponent(content)}`}
              download={`${component}.${locale}.md`}
            />
          }
        >
          {t(($) => $.rules.content.download)}
        </Button>
      </div>
      <Markdown mode="full">{content}</Markdown>
    </details>
  );
}
