import { LabI18nProvider } from "./lab-i18n";
import { useTranslation } from "react-i18next";
import { useEffect, useState, type ReactNode } from "react";
import { ArrowUpRight, Check, Inbox, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Switch } from "@multica/ui/components/ui/switch";
import { Avatar, AvatarFallback } from "@multica/ui/components/ui/avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { StatusIcon } from "@multica/views/issues/visuals";
import { ColorsScene } from "./colors-scene";
import { DialogScene } from "./dialog-scene";
import { ButtonScene } from "./button-scene";
import { ProductPreview } from "./product-preview";
import { emptyDraft, previewCss } from "./tokens";
import { isPreviewSettings, type PreviewSettings } from "./protocol";

function Specimen({
  title,
  caption,
  children,
  wide = false,
}: {
  title: string;
  caption: string;
  children: ReactNode;
  wide?: boolean;
}) {
  return (
    <section className={`specimen ${wide ? "specimen-wide" : ""}`}>
      <header>
        <h2>{title}</h2>
        <span>{caption}</span>
      </header>
      <div className="specimen-body">{children}</div>
    </section>
  );
}
function Person({ name = "JZ" }: { name?: string }) {
  return (
    <Avatar size="sm">
      <AvatarFallback>{name}</AvatarFallback>
    </Avatar>
  );
}
function SurfaceSwatches() {
  const { t } = useTranslation("uiLab");
  const surfaces = [
    ["--app-shell", t(($) => $.gallery.surfaces.shell)],
    ["--page-canvas", t(($) => $.gallery.surfaces.page)],
    ["--surface", t(($) => $.gallery.surfaces.surface)],
    ["--surface-raised", t(($) => $.gallery.surfaces.raised)],
    ["--surface-hover", t(($) => $.gallery.surfaces.hover)],
    ["--surface-selected", t(($) => $.gallery.surfaces.selected)],
  ];
  return (
    <div className="surface-swatches">
      {surfaces.map(([key, name]) => (
        <div key={key}>
          <div style={{ background: `var(${key})` }}>Aa</div>
          <span>{name}</span>
        </div>
      ))}
    </div>
  );
}
function ExampleDialog() {
  const { t } = useTranslation("uiLab");
  return (
    <Dialog>
      <DialogTrigger render={<Button variant="outline" />}>
        {t(($) => $.gallery.dialog.open)}
        <ArrowUpRight />
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.gallery.dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.gallery.dialog.description)}
          </DialogDescription>
        </DialogHeader>
        <Input
          aria-label={t(($) => $.gallery.dialog.label)}
          placeholder={t(($) => $.gallery.dialog.placeholder)}
        />
      </DialogContent>
    </Dialog>
  );
}
function ComponentsScene() {
  const { t } = useTranslation("uiLab");
  return (
    <div className="component-scene">
      <div className="scene-intro">
        <h1>{t(($) => $.gallery.sections.title)}</h1>
      </div>
      <div className="specimen-grid">
        <Specimen
          title={t(($) => $.gallery.sections.surfaces)}
          caption="SURFACES"
          wide
        >
          <SurfaceSwatches />
        </Specimen>
        <Specimen title={t(($) => $.button.page.title)} caption="BUTTON">
          <div className="flex flex-wrap gap-3">
            <Button>{t(($) => $.button.actions.create)}</Button>
            <Button variant="brand">
              <Plus />
              {t(($) => $.gallery.actions.new)}
            </Button>
            <Button variant="secondary">
              {t(($) => $.button.variants.secondary)}
            </Button>
            <Button variant="outline">
              {t(($) => $.gallery.actions.outline)}
            </Button>
            <Button variant="ghost">{t(($) => $.button.variants.ghost)}</Button>
            <Button variant="destructive">
              {t(($) => $.gallery.actions.delete)}
            </Button>
            <Button disabled>{t(($) => $.gallery.actions.disabled)}</Button>
          </div>
        </Specimen>
        <Specimen title={t(($) => $.gallery.sections.forms)} caption="FORM">
          <div className="grid gap-4">
            <Input
              aria-label={t(($) => $.gallery.form.title)}
              placeholder={t(($) => $.gallery.form.placeholder)}
            />
            <div className="flex items-center justify-between gap-4">
              <label className="flex items-center gap-2 text-body">
                <Checkbox defaultChecked />
                {t(($) => $.gallery.form.notify)}
              </label>
              <label className="flex items-center gap-2 text-body">
                {t(($) => $.gallery.form.autopilot)}
                <Switch defaultChecked />
              </label>
            </div>
            <Input
              aria-label={t(($) => $.gallery.form.disabled)}
              disabled
              placeholder={t(($) => $.gallery.form.readonly)}
            />
          </div>
        </Specimen>
        <Specimen title={t(($) => $.gallery.sections.status)} caption="STATUS">
          <div className="flex flex-wrap gap-x-5 gap-y-4">
            {[
              "backlog",
              "todo",
              "in_progress",
              "in_review",
              "done",
              "blocked",
            ].map((status) => (
              <span
                key={status}
                className="flex items-center gap-2 text-caption"
              >
                <StatusIcon status={status} />
                {status}
              </span>
            ))}
          </div>
          <div className="mt-5 flex flex-wrap gap-2">
            <Badge>Design</Badge>
            <Badge variant="secondary">Frontend</Badge>
            <Badge variant="outline">v0.4.0</Badge>
            <Badge variant="destructive">
              {t(($) => $.gallery.form.attention)}
            </Badge>
          </div>
        </Specimen>
        <Specimen
          title={t(($) => $.gallery.sections.typography)}
          caption="TYPOGRAPHY"
        >
          <div className="grid gap-2">
            <h3 className="text-title-lg font-semibold">
              {t(($) => $.gallery.type.heading)}
            </h3>
            <p className="text-body">{t(($) => $.gallery.type.body)}</p>
            <p className="text-label">{t(($) => $.gallery.type.label)}</p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.gallery.type.caption)}
            </p>
          </div>
        </Specimen>
        <Specimen
          title={t(($) => $.gallery.sections.elevation)}
          caption="ELEVATION"
        >
          <div className="rounded-xl border border-surface-border bg-surface p-4 shadow-[var(--surface-shadow)]">
            <div className="mb-4 flex items-center gap-3">
              <Person />
              <div>
                <p className="text-body font-medium">
                  {t(($) => $.gallery.type.cardTitle)}
                </p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.gallery.type.cardDescription)}
                </p>
              </div>
            </div>
            <ExampleDialog />
          </div>
        </Specimen>
        <Specimen
          title={t(($) => $.gallery.sections.feedback)}
          caption="FEEDBACK"
        >
          <div className="flex items-center gap-3">
            <Skeleton className="size-8 rounded-full" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-3 w-3/5" />
              <Skeleton className="h-3 w-2/5" />
            </div>
          </div>
          <div className="mt-5 flex items-center gap-3 text-muted-foreground">
            <Inbox className="size-5" />
            <span className="text-body">{t(($) => $.gallery.type.empty)}</span>
            <Check className="ml-auto size-4 text-success" />
          </div>
        </Specimen>
      </div>
    </div>
  );
}
export function Preview() {
  const [settings, setSettings] = useState<PreviewSettings>({
    type: "multica-ui-lab:preview",
    draft: emptyDraft(),
    theme: "light",
    scene: "components",
    buttonScale: "default",
    locale: "en",
    playbackSpeed: 1,
    selectedColor: "--brand",
  });
  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (
        event.origin === location.origin &&
        event.source === window.parent &&
        isPreviewSettings(event.data)
      )
        setSettings(event.data);
    };
    window.addEventListener("message", receive);
    window.parent.postMessage(
      { type: "multica-ui-lab:ready" },
      location.origin,
    );
    return () => window.removeEventListener("message", receive);
  }, []);
  useEffect(() => {
    document.documentElement.classList.toggle(
      "dark",
      settings.theme === "dark",
    );
  }, [settings.theme]);
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [settings.scene]);
  return (
    <LabI18nProvider locale={settings.locale}>
      <div className="preview-root">
        <style>{previewCss(settings.draft)}</style>
        {settings.scene === "colors" ? (
          <ColorsScene
            draft={settings.draft}
            theme={settings.theme}
            selected={settings.selectedColor}
          />
        ) : settings.scene === "components" ? (
          <ComponentsScene />
        ) : settings.scene === "button" ? (
          <ButtonScene scale={settings.buttonScale} draft={settings.draft} />
        ) : settings.scene === "dialog" || settings.scene === "motion" ? (
          <DialogScene
            key={settings.scene}
            motion={settings.scene === "motion"}
            draft={settings.draft}
            speed={settings.playbackSpeed}
          />
        ) : settings.scene === "list" ? (
          <ProductPreview key="list" scene="list" locale={settings.locale} />
        ) : (
          <ProductPreview
            key="detail"
            scene="detail"
            locale={settings.locale}
          />
        )}
      </div>
    </LabI18nProvider>
  );
}
