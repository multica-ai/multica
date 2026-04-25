export const dynamic = "force-static";

export const metadata = {
  title: "Offline",
  robots: { index: false, follow: false },
};

export default function OfflinePage() {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        height: "100vh",
        fontFamily: "system-ui, -apple-system, sans-serif",
        gap: "0.75rem",
        padding: "2rem",
        textAlign: "center",
      }}
    >
      <h1 style={{ fontSize: "1.25rem", fontWeight: 600, margin: 0 }}>
        You&apos;re offline
      </h1>
      <p style={{ color: "#6b7280", margin: 0 }}>
        Multica needs a connection to load workspace data. Reconnect and try again.
      </p>
    </div>
  );
}
