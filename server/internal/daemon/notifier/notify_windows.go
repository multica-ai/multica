//go:build windows

package notifier

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type platformNotifier struct{}

func New() Notifier {
	return platformNotifier{}
}

func (platformNotifier) Notify(ctx context.Context, title, body string) error {
	ps := buildPowerShellToastScript(title, body)
	return exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", ps).Run()
}

func buildPowerShellToastScript(title, body string) string {
	escapedTitle := strings.ReplaceAll(title, `'`, `''`)
	escapedBody := strings.ReplaceAll(body, `'`, `''`)
	return fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] > $null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml("<toast><visual><binding template='ToastText02'><text id='1'>%s</text><text id='2'>%s</text></binding></visual></toast>")
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('Multica')
$notifier.Show($toast)
`, escapedTitle, escapedBody)
}
