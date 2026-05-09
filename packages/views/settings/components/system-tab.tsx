"use client";

import React from "react";
import { ImageIcon, Save, Camera, Loader2, Upload } from "lucide-react";
import { useT } from "../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@multica/ui/components/ui/card";
import { useConfigStore } from "@multica/core/config";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { toast } from "@multica/ui/components/ui/sonner";

export function SystemTab() {
  const { t } = useT("settings");
  const { customLogoUrl, loginText, setAuthConfig } = useConfigStore();
  const [logoUrl, setLogoUrl] = React.useState(customLogoUrl);
  const [text, setText] = React.useState(loginText);
  const [isSaving, setIsSaving] = React.useState(false);
  const { upload, uploading } = useFileUpload(api);
  const fileInputRef = React.useRef<HTMLInputElement>(null);

  const handleSave = async () => {
    setIsSaving(true);
    try {
      await api.updateSystemSettings([
        { key: "custom_logo_url", value: logoUrl },
        { key: "login_page_text", value: text },
      ]);
      setAuthConfig({
        allowSignup: useConfigStore.getState().allowSignup,
        googleClientId: useConfigStore.getState().googleClientId,
        customLogoUrl: logoUrl,
        loginText: text,
      });
      toast.success(t(($) => $.system.success_toast));
    } catch (err) {
      toast.error(t(($) => $.system.error_toast));
    } finally {
      setIsSaving(false);
    }
  };

  const handleLogoUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
    try {
      const result = await upload(file);
      if (result) {
        setLogoUrl(result.link);
      }
    } catch (err) {
      toast.error(t(($) => $.system.error_toast));
    }
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ImageIcon className="h-5 w-5" />
            {t(($) => $.system.branding_title)}
          </CardTitle>
          <CardDescription>
            {t(($) => $.system.branding_description)}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <label className="text-sm font-medium">{t(($) => $.system.logo_label)}</label>
            <div className="flex items-start gap-4">
              <button
                type="button"
                className="group relative h-16 w-32 shrink-0 rounded-md bg-muted border overflow-hidden focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                onClick={() => fileInputRef.current?.click()}
                disabled={uploading}
              >
                {logoUrl ? (
                  <img
                    src={logoUrl}
                    alt="Logo preview"
                    className="h-full w-full object-contain p-2"
                  />
                ) : (
                  <div className="flex h-full w-full items-center justify-center text-xs font-medium text-muted-foreground gap-1 flex-col">
                    <ImageIcon className="h-4 w-4" />
                    Preview
                  </div>
                )}
                <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
                  {uploading ? (
                    <Loader2 className="h-5 w-5 animate-spin text-white" />
                  ) : (
                    <Camera className="h-5 w-5 text-white" />
                  )}
                </div>
              </button>
              <div className="flex-1 space-y-2">
                <div className="flex gap-2">
                  <Input
                    placeholder="https://example.com/logo.png"
                    value={logoUrl}
                    onChange={(e) => setLogoUrl(e.target.value)}
                  />
                  <Button
                    variant="outline"
                    onClick={() => fileInputRef.current?.click()}
                    disabled={uploading}
                    className="shrink-0"
                  >
                    <Upload className="h-4 w-4" />
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.system.logo_hint)}
                </p>
              </div>
            </div>
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*"
              className="hidden"
              onChange={handleLogoUpload}
            />
          </div>

          <div className="space-y-2">
            <label className="text-sm font-medium">{t(($) => $.system.text_label)}</label>
            <Textarea
              placeholder={t(($) => $.system.text_placeholder)}
              value={text}
              onChange={(e) => setText(e.target.value)}
              rows={3}
            />
            <p className="text-xs text-muted-foreground">
              {t(($) => $.system.text_hint)}
            </p>
          </div>

          <div className="pt-4 flex justify-end">
            <Button onClick={handleSave} disabled={isSaving}>
              <Save className="h-4 w-4 mr-2" />
              {isSaving ? t(($) => $.system.saving) : t(($) => $.system.save_button)}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
