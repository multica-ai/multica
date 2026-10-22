import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import { ColorUsage } from "./color-usage";
import { Input } from "@multica/ui/components/ui/input";
import { colorToHex } from "./color";
import {
  colorGroups,
  colorAlpha,
  colorAliases,
  tokenValue,
  type ColorToken,
  type Draft,
  type Theme,
} from "./tokens";
import type { ColorSelection } from "./protocol";

export function ColorsScene({
  draft,
  theme,
  selected,
}: {
  draft: Draft;
  theme: Theme;
  selected: ColorToken;
}) {
  const { t } = useTranslation("uiLab");
  const [query, setQuery] = useState("");
  const [section, setSection] = useState("palette");
  const groups = colorGroups
    .map((group) => ({
      id: group.id,
      tokens: group.tokens.filter(([key, label]) =>
        `${key} ${t(($) => $.tokens.labels[label])}`
          .toLowerCase()
          .includes(query.trim().toLowerCase()),
      ),
    }))
    .filter((group) => group.tokens.length > 0);
  const select = (token: ColorToken) =>
    window.parent.postMessage(
      { type: "multica-ui-lab:color-select", token } satisfies ColorSelection,
      location.origin,
    );
  return (
    <div className="colors-scene">
      <header className="palette-header">
        <div>
          <h1>{t(($) => $.palette.page.title)}</h1>
          <span>
            {t(($) => $.palette.page.count, {
              count: groups.reduce(
                (count, group) => count + group.tokens.length,
                0,
              ),
            })}
          </span>
        </div>
        {section === "palette" && (
          <Input
            type="search"
            aria-label={t(($) => $.palette.page.search)}
            placeholder={t(($) => $.palette.page.placeholder)}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        )}
      </header>
      <Tabs
        value={section}
        onValueChange={(value) => setSection(String(value))}
      >
        <TabsList aria-label={t(($) => $.colorUsage.sections)}>
          <TabsTrigger value="palette">
            {t(($) => $.colorUsage.palette)}
          </TabsTrigger>
          <TabsTrigger value="usage">
            {t(($) => $.colorUsage.title)}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="usage">
          <ColorUsage draft={draft} theme={theme} select={select} />
        </TabsContent>
        <TabsContent value="palette">
          {groups.map((group) => (
            <section
              className="palette-group"
              key={group.id}
              aria-labelledby={`palette-${group.id}`}
            >
              <h2 id={`palette-${group.id}`}>
                {t(($) => $.palette.groups[group.id])}
              </h2>
              <div className="palette-grid">
                {group.tokens.map(([key, label]) => {
                  const value = tokenValue(draft, theme, key);
                  const name = t(($) => $.tokens.labels[label]);
                  return (
                    <button
                      type="button"
                      className="palette-token"
                      key={key}
                      aria-label={t(($) => $.palette.page.edit, {
                        label: name,
                      })}
                      aria-pressed={selected === key}
                      onClick={() => select(key)}
                    >
                      <span
                        className="palette-swatch"
                        style={{ backgroundColor: value }}
                      />
                      <span className="palette-token-heading">
                        <strong>{name}</strong>
                        {draft[theme][key] && (
                          <span
                            className="property-modified"
                            title={t(($) => $.inspector.changes.modified)}
                            aria-label={t(($) => $.inspector.changes.modified)}
                          />
                        )}
                      </span>
                      <code>{key}</code>
                      <span className="palette-hex">
                        {colorToHex(value)}
                        {colorAlpha(value) < 1 &&
                          ` · ${Number((colorAlpha(value) * 100).toFixed(1))}%`}
                      </span>
                    </button>
                  );
                })}
              </div>
            </section>
          ))}
          {!groups.length && (
            <p className="palette-empty" role="status">
              {t(($) => $.palette.page.empty)}
            </p>
          )}
          {!query.trim() && (
            <>
              <section
                className="palette-group"
                aria-labelledby="palette-aliases"
              >
                <h2 id="palette-aliases">{t(($) => $.palette.page.aliases)}</h2>
                <div className="palette-aliases">
                  {colorAliases.map(([alias, target]) => (
                    <button
                      key={alias}
                      type="button"
                      onClick={() => select(target)}
                    >
                      <code>{alias}</code>
                      <span aria-hidden="true">→</span>
                      <code>{target}</code>
                    </button>
                  ))}
                </div>
              </section>
            </>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
