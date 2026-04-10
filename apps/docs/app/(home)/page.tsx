import Link from "next/link";

export default function HomePage() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-6 text-center px-4">
      <h1 className="text-4xl font-bold tracking-tight sm:text-5xl">
        Multica Documentation
      </h1>
      <p className="max-w-2xl text-lg text-fd-muted-foreground">
        The open-source managed agents platform. Turn coding agents into real
        teammates — assign tasks, track progress, compound skills.
      </p>
      <div className="flex gap-4">
        <Link
          href="/docs"
          className="inline-flex items-center rounded-md bg-fd-primary px-6 py-3 text-sm font-medium text-fd-primary-foreground transition-colors hover:bg-fd-primary/90"
        >
          Get Started
        </Link>
        <Link
          href="https://github.com/multica-ai/multica"
          className="inline-flex items-center rounded-md border border-fd-border px-6 py-3 text-sm font-medium transition-colors hover:bg-fd-accent"
        >
          GitHub
        </Link>
      </div>
    </main>
  );
}
