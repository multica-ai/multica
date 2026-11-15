import { forwardRef, type ReactNode } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Bell, Check, FolderMinus, Inbox, Layers, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useStoryEnv } from "../voice-input/env";
import { UI, type Update, type UpdateId } from "./copy";
import { HEROES } from "./heroes";

/** Same frame as the star prompt: it shares the corner above the chat launcher. */
export const CARD_FRAME =
  "w-[23.75rem] overflow-hidden rounded-xl bg-surface-raised text-popover-foreground shadow-[var(--floating-shadow)] ring-1 ring-floating-border";

export const UPDATE_ICON: Record<UpdateId, typeof Inbox> = {
  inbox: Inbox,
  views: Layers,
  "no-project": FolderMinus,
  notifications: Bell,
};

export function CloseButton({ onClick, onHero = false }: { onClick?: () => void; onHero?: boolean }) {
  const { lang } = useStoryEnv();
  return (
    <Button
      variant="ghost"
      size="icon-xs"
      aria-label={UI.close[lang]}
      onClick={(e) => {
        e.stopPropagation();
        onClick?.();
      }}
      className={cn(
        "text-muted-foreground",
        onHero && "bg-surface-raised/90 shadow-sm backdrop-blur hover:bg-surface-raised",
      )}
    >
      <X />
    </Button>
  );
}

export function Meta({ update }: { update: Update }) {
  const { lang } = useStoryEnv();
  return (
    <div className="flex items-center gap-2 text-micro">
      <span className="rounded-sm bg-brand/10 px-1.5 py-0.5 font-medium text-brand">{UI.tag[lang]}</span>
      <span className="text-muted-foreground">{update.date[lang]}</span>
    </div>
  );
}

export function UpdateBody({ update }: { update: Update }) {
  const { lang } = useStoryEnv();
  return (
    <>
      <Meta update={update} />
      <h2 className="mt-2 text-body-lg font-semibold">{update.title[lang]}</h2>
      <p className="mt-1 text-label text-muted-foreground">{update.body[lang]}</p>
      <ul className="mt-3 space-y-1.5">
        {update.bullets.map((b) => (
          <li key={b.en} className="flex gap-2 text-label">
            <Check className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
            <span>{b[lang]}</span>
          </li>
        ))}
      </ul>
    </>
  );
}

export function Hero({ id }: { id: UpdateId }) {
  const Art = HEROES[id];
  return <Art />;
}

interface UpdateContentProps {
  update: Update;
  footer: ReactNode;
  onClose?: () => void;
  /** Replaces the hero, e.g. a carousel strip. */
  hero?: ReactNode;
  /** Replaces the text, e.g. to animate it per page. */
  body?: ReactNode;
}

/** A card's insides without its frame, so a stack can clip and measure it. */
export const UpdateContent = forwardRef<HTMLDivElement, UpdateContentProps>(function UpdateContent(
  { update, footer, onClose, hero, body },
  ref,
) {
  return (
    <div ref={ref}>
      <div className="relative">
        {hero ?? <Hero id={update.id} />}
        {onClose && (
          <div className="absolute top-2.5 right-2.5">
            <CloseButton onClick={onClose} onHero />
          </div>
        )}
      </div>
      <div className="px-5 pt-4 pb-4">
        {body ?? <UpdateBody update={update} />}
        <div className="mt-4">{footer}</div>
      </div>
    </div>
  );
});

export function UpdateCard({ className, ...props }: UpdateContentProps & { className?: string }) {
  return (
    <section aria-label={props.update.title.en} className={cn(CARD_FRAME, className)}>
      <UpdateContent {...props} />
    </section>
  );
}

export function Dots({ count, index, onSelect }: { count: number; index: number; onSelect?: (i: number) => void }) {
  return (
    <div className="flex items-center gap-1">
      {Array.from({ length: count }, (_, i) => (
        <button
          key={i}
          type="button"
          aria-label={`${i + 1} / ${count}`}
          aria-current={i === index}
          onClick={() => onSelect?.(i)}
          className={cn(
            // eslint-disable-next-line no-restricted-syntax -- page dots
            "h-1.5 rounded-full transition-[width,background-color] duration-200",
            i === index ? "w-4 bg-foreground" : "w-1.5 bg-muted-foreground/30 hover:bg-muted-foreground/60",
          )}
        />
      ))}
    </div>
  );
}

/** One line per update: the collapsed form of option B. */
export function CompactCard({
  update,
  onOpen,
  onClose,
  className,
  style,
  inert,
}: {
  update: Update;
  onOpen?: () => void;
  onClose?: () => void;
  className?: string;
  style?: React.CSSProperties;
  inert?: boolean;
}) {
  const { lang } = useStoryEnv();
  const Icon = UPDATE_ICON[update.id];
  return (
    <div
      inert={inert}
      className={cn(CARD_FRAME, "flex h-[4.5rem] items-center gap-3 px-3 text-left", className)}
      style={style}
    >
      <button type="button" onClick={onOpen} className="flex min-w-0 flex-1 items-center gap-3 text-left outline-none">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-brand text-brand-foreground">
          <Icon className="size-5" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5 text-micro">
            <span className="font-medium text-brand">{UI.tag[lang]}</span>
            <span className="text-muted-foreground">{update.date[lang]}</span>
          </span>
          <span className="block truncate text-body font-semibold">{update.title[lang]}</span>
          <span className="block truncate text-caption text-muted-foreground">{update.body[lang]}</span>
        </span>
      </button>
      {onClose && <CloseButton onClick={onClose} />}
    </div>
  );
}
