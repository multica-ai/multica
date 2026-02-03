export default function OfflinePage() {
  return (
    <div
      className="flex h-dvh w-full items-center justify-center"
      style={{ display: "flex", height: "100dvh", width: "100%", alignItems: "center", justifyContent: "center" }}
    >
      <div
        className="flex flex-col items-center gap-4 text-center"
        style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: "1rem", textAlign: "center" }}
      >
        <div className="text-4xl" style={{ fontSize: "2.25rem" }}>*</div>
        <h1 className="text-xl font-semibold" style={{ fontSize: "1.25rem", fontWeight: 600 }}>
          You are offline
        </h1>
        <p className="text-sm text-muted-foreground" style={{ fontSize: "0.875rem", color: "#a1a1aa" }}>
          Multica requires an internet connection. Please check your network and
          try again.
        </p>
      </div>
    </div>
  );
}
