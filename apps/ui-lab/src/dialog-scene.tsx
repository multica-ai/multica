import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { ComponentRules } from "./component-rules";
import { isDialogCommand } from "./protocol";
import { motionTokens, easingTokens, tokenValue, type Draft } from "./tokens";

export function DialogScene({
  motion,
  draft,
  speed,
}: {
  motion: boolean;
  draft: Draft;
  speed: number;
}) {
  const { t } = useTranslation("uiLab");
  const [open, setOpen] = useState(false);
  const [example, setExample] = useState("form");
  const [title, setTitle] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [reduced, setReduced] = useState(false);
  const replay = useRef(false);
  const openRef = useRef(open);
  openRef.current = open;
  useEffect(() => {
    const query = matchMedia("(prefers-reduced-motion: reduce)");
    const sync = () => setReduced(query.matches);
    sync();
    query.addEventListener("change", sync);
    return () => query.removeEventListener("change", sync);
  }, []);
  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (
        event.origin !== location.origin ||
        event.source !== window.parent ||
        !isDialogCommand(event.data)
      )
        return;
      if (event.data.action !== "close") setSaved(false);
      replay.current = event.data.action === "replay" && openRef.current;
      setOpen(
        event.data.action === "open" ||
          (event.data.action === "replay" && !openRef.current),
      );
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, []);
  const selectors =
    ':root [data-slot="dialog-content"], :root [data-slot="dialog-overlay"]';
  return (
    <div className="button-scene dialog-scene">
      <style>{`${selectors} { --tw-animation-duration: calc(var(--dialog-enter-duration) / ${speed}); } :root [data-slot="dialog-content"][data-closed], :root [data-slot="dialog-overlay"][data-closed] { --tw-animation-duration: calc(var(--dialog-exit-duration) / ${speed}); }`}</style>
      <header className="button-intro">
        <h1>{t(($) => $.system.pages[motion ? "motion" : "dialog"])}</h1>
        {motion && <code>Dialog</code>}
      </header>
      {reduced && (
        <p className="motion-reduced" role="status">
          {t(($) => $.motion.controls.reduced)}
        </p>
      )}
      <section className="button-section">
        <div className="button-playground-controls dialog-options">
          <label>
            {t(($) => $.dialog.controls.example)}
            <select
              value={example}
              onChange={(event) => {
                setExample(event.target.value);
                setSaved(false);
              }}
            >
              <option value="form">{t(($) => $.dialog.controls.form)}</option>
              <option value="long">{t(($) => $.dialog.controls.long)}</option>
            </select>
          </label>
        </div>
        <div className="button-stage">
          <Dialog
            open={open}
            onOpenChange={(value) => {
              if (value) setSaved(false);
              replay.current = false;
              setOpen(value);
            }}
            onOpenChangeComplete={(value) => {
              if (!value && replay.current) {
                replay.current = false;
                setOpen(true);
              }
            }}
          >
            <DialogTrigger render={<Button />}>
              {t(($) => $.dialog.actions.open)}
            </DialogTrigger>
            <DialogContent showCloseButton={false}>
              <DialogHeader>
                <DialogTitle>{t(($) => $.dialog.content.title)}</DialogTitle>
                <DialogDescription>
                  {t(($) => $.dialog.content.description)}
                </DialogDescription>
              </DialogHeader>
              <form
                className="dialog-example-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  setSaved(true);
                  replay.current = false;
                  setOpen(false);
                }}
              >
                <label>
                  {t(($) => $.dialog.content.label)}
                  <Input
                    required
                    value={title ?? t(($) => $.dialog.content.defaultTitle)}
                    onChange={(event) => setTitle(event.target.value)}
                  />
                </label>
                {example === "long" && (
                  <div className="dialog-long-content">
                    {Array.from({ length: 12 }, (_, index) => (
                      <p key={index}>
                        {t(($) => $.dialog.content.item, { number: index + 1 })}
                      </p>
                    ))}
                  </div>
                )}
                <DialogFooter>
                  <DialogClose
                    render={<Button type="button" variant="outline" />}
                  >
                    {t(($) => $.dialog.actions.cancel)}
                  </DialogClose>
                  <Button type="submit">
                    {t(($) => $.dialog.actions.save)}
                  </Button>
                </DialogFooter>
              </form>
            </DialogContent>
          </Dialog>
          <p role="status">
            {t(
              ($) =>
                $.dialog.states[saved ? "saved" : open ? "open" : "closed"],
            )}
          </p>
        </div>
        <div className="motion-token-list">
          {[...motionTokens, ...easingTokens].map((token) => (
            <div key={token.key}>
              <span>{t(($) => $.tokens.labels[token.label])}</span>
              <code>{tokenValue(draft, "shared", token.key)}</code>
            </div>
          ))}
        </div>
      </section>
      <ComponentRules component="dialog" />
    </div>
  );
}
