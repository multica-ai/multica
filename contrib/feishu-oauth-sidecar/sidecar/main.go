package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := loadConfig()
	runner := &CommandRunner{ScriptsDir: cfg.ScriptsDir}

	if err := initMini(logger, runner); err != nil {
		logger.Error("init failed", "error", err)
		os.Exit(1)
	}

	srv := NewServer(cfg, runner, logger)
	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 210 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// expiry watcher 后台 goroutine: 每 1h 扫一次,token 距 refresh expiry <24h 发飞书提醒,
	// 24h cooldown 防止重复打扰. ctx 在 main 退出时取消.
	watcherCtx, watcherCancel := context.WithCancel(context.Background())
	defer watcherCancel()
	stateFile := filepath.Join(filepath.Dir(cfg.MappingFile), "notified.json")
	watcher := NewExpiryWatcher(runner, logger, cfg.MappingFile, stateFile)
	watcher.Start(watcherCtx)

	go func() {
		logger.Info("server start", "addr", httpServer.Addr, "expiry_watcher", "enabled")
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	watcherCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("shutdown failed", "error", err)
	}
	logger.Info("server stopped")
}

func loadConfig() Config {
	mapping := expandHome(getEnv("MAPPING_FILE", "~/multica-user-homes/user_mapping.json"))
	return Config{
		Port:           getEnv("PORT", "18090"),
		ScriptsDir:     expandHome(getEnv("SCRIPTS_DIR", "~/lark_multi_user")),
		MappingFile:    mapping,
		UserHomesDir:   expandHome(getEnv("USER_HOMES_DIR", filepath.Dir(mapping))),
		AllowedOrigins: splitCSV(getEnv("ALLOWED_ORIGINS", "*")),
		Version:        version,
	}
}

func initMini(logger *slog.Logger, runner ScriptRunner) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runner.Run(ctx, "01_mini_init.sh")
	if err != nil {
		return err
	}
	logger.Info("init done", "stdout", strings.TrimSpace(res.Stdout))
	if strings.TrimSpace(res.Stderr) != "" {
		logger.Warn("init stderr", "stderr", strings.TrimSpace(res.Stderr))
	}
	return nil
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}

func requestUserID(r *http.Request) string {
	if r.Method == http.MethodGet {
		return r.URL.Query().Get("multica_user_id")
	}
	return ""
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
