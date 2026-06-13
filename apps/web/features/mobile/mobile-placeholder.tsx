export function MobilePlaceholderPage({ title }: { title: string }) {
  return (
    <section className="min-h-full px-4 pb-8 pt-5">
      <div className="border-b border-border pb-5">
        <p className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
          Mobile foundation
        </p>
        <h1 className="mt-2 text-2xl font-semibold text-foreground">
          {title}
        </h1>
      </div>
      <div className="flex min-h-[55svh] items-center justify-center text-center">
        <p className="text-base font-medium text-muted-foreground">
          ここに {title} が来ます
        </p>
      </div>
    </section>
  );
}
