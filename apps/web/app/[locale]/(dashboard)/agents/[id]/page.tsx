export default function AgentDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  return (
    <div className="p-6">
      <h1 className="text-2xl font-bold">Agent Detail</h1>
      <p className="mt-2 text-muted-foreground">Agent status and task history</p>
    </div>
  );
}
