import Link from "next/link";

export default function NotFound() {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-4 px-4 py-24 text-center">
      <h1 className="text-3xl font-semibold">Page not found</h1>
      <p className="text-fd-muted-foreground">
        The page you are looking for doesn&apos;t exist.
      </p>
      <Link
        href="/"
        className="inline-flex items-center rounded-md bg-fd-primary px-4 py-2 text-sm font-medium text-fd-primary-foreground transition-colors hover:bg-fd-primary/90"
      >
        Back to docs
      </Link>
    </main>
  );
}
