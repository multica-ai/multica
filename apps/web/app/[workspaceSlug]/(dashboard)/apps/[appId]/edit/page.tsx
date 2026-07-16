import { AppEditorPage } from "@multica/cerebro-apps";

export default async function AppEditorRoute({ params }: { params: Promise<{ appId: string }> }) {
  const { appId } = await params;
  return <AppEditorPage appId={appId} />;
}
