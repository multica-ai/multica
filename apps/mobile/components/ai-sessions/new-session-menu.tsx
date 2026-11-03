import { useSafeAreaInsets } from "react-native-safe-area-context";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { IconButton } from "@/components/ui/icon-button";
import { Text } from "@/components/ui/text";
import { ProviderLogo } from "@/components/ai-sessions/provider-logo";
import { useT } from "@/lib/i18n";

interface NewSessionMenuProps {
  providers: { id: string; name: string; available: boolean }[];
  disabled: boolean;
  onSelect: (provider: string) => void;
}

export function NewSessionMenu({ providers, disabled, onSelect }: NewSessionMenuProps) {
  const { t } = useT("aiSessions");
  const insets = useSafeAreaInsets();
  const available = providers.filter((provider) => provider.available);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={disabled}>
        <IconButton
          name="add"
          hitSlop={4}
          accessibilityLabel={t("new")}
        />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="bottom"
        align="end"
        sideOffset={8}
        insets={{ ...insets, left: insets.left + 16, right: insets.right + 16 }}
        className="w-60"
        accessibilityLabel={t("provider")}
      >
        {available.length ? available.map((provider) => (
          <DropdownMenuItem
            key={provider.id}
            disabled={disabled}
            onPress={() => onSelect(provider.id)}
            accessibilityLabel={provider.name}
          >
            <ProviderLogo provider={provider.id} size={20} />
            <Text style={{ lineHeight: undefined }} className="flex-1">
              {provider.name}
            </Text>
          </DropdownMenuItem>
        )) : (
          <Text style={{ lineHeight: undefined }} className="px-3 py-3 text-sm text-muted-foreground">
            {t("no_provider")}
          </Text>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
