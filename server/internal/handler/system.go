package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
)

type ChooseDirectoryRequest struct {
	Prompt string `json:"prompt"`
}

type ChooseDirectoryResponse struct {
	Path string `json:"path"`
}

func (h *Handler) ChooseDirectory(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "directory chooser is only available from localhost")
		return
	}

	if runtime.GOOS != "darwin" {
		writeError(w, http.StatusNotImplemented, "directory chooser is only supported on macOS")
		return
	}

	var req ChooseDirectoryRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "Choose a folder"
	}

	script := fmt.Sprintf(`POSIX path of (choose folder with prompt %q)`, prompt)
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(msg, "-128") {
				writeError(w, http.StatusConflict, "directory selection was cancelled")
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "failed to choose directory")
		return
	}

	path := strings.TrimSpace(string(out))
	if path == "" {
		writeError(w, http.StatusInternalServerError, "directory chooser returned an empty path")
		return
	}

	writeJSON(w, http.StatusOK, ChooseDirectoryResponse{Path: path})
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}
