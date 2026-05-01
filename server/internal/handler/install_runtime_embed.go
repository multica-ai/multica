package handler

import (
	_ "embed"
	"strings"
)

//go:embed install_runtime.sh
var installRuntimeScript string

func renderInstallScript(serverURL string) string {
	return strings.ReplaceAll(installRuntimeScript, "__SERVER_URL__", strings.TrimRight(serverURL, "/"))
}
