import { ArrowUpRight, LifeBuoy } from "lucide-react";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import {
  DESKTOP_BROWSER_LOGIN_URL,
  CLI_AND_DAEMON_GUIDE_URL,
} from "../support-links";

export function DesktopLoginPage() {
  const openBrowserLogin = () => {
    window.desktopAPI.openExternal(DESKTOP_BROWSER_LOGIN_URL);
  };

  return (
    <div className="flex h-screen flex-col bg-background">
      {/* Traffic light inset */}
      <div
        className="h-[38px] shrink-0"
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      />
      <div className="flex flex-1 items-center justify-center px-6 pb-10">
        <Card className="w-full max-w-md">
          <CardHeader className="items-center text-center">
            <MulticaIcon bordered size="lg" />
            <CardTitle className="mt-4 text-2xl">Continue in your browser</CardTitle>
            <CardDescription className="max-w-sm">
              This desktop build now prefers trusted bootstrap or browser-based
              sign-in. If bootstrap is unavailable, finish sign-in on the web and
              Multica will hand the session back to the app automatically.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Button onClick={openBrowserLogin} className="w-full">
              Open browser sign-in
              <ArrowUpRight className="size-4" />
            </Button>
            <Button
              variant="outline"
              className="w-full"
              onClick={() => window.desktopAPI.openExternal(CLI_AND_DAEMON_GUIDE_URL)}
            >
              Troubleshooting
              <LifeBuoy className="size-4" />
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
