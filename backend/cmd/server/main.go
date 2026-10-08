package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"support-ticket-system/backend/internal/auth"
	"support-ticket-system/backend/internal/config"
	db "support-ticket-system/backend/internal/db"
	httpapi "support-ticket-system/backend/internal/http"
	"support-ticket-system/backend/internal/realtime"
	"support-ticket-system/backend/internal/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := seedAdmin(ctx, pool, cfg); err != nil {
		slog.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}

	store, err := buildStorage(ctx, cfg)
	if err != nil {
		slog.Error("storage startup failed", "error", err)
		os.Exit(1)
	}

	hub := realtime.NewHub()
	go hub.Run()
	app := httpapi.NewApp(cfg, pool, hub, store)
	server := &http.Server{Addr: ":" + cfg.Port, Handler: app.Router(), ReadHeaderTimeout: 10 * time.Second}

	go func() {
		slog.Info("support ticket API listening", "port", cfg.Port, "storage", cfg.StorageDriver)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server stopped", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func seedAdmin(ctx context.Context, pool *pgxpool.Pool, cfg config.Config) error {
	if strings.TrimSpace(cfg.SeedAdminEmail) == "" || strings.TrimSpace(cfg.SeedAdminPass) == "" {
		return nil
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1))`, cfg.SeedAdminEmail).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	hash, err := auth.HashPassword(cfg.SeedAdminPass)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO users(full_name,email,password_hash,role) VALUES($1,$2,$3,'support')`, cfg.SeedAdminName, strings.ToLower(strings.TrimSpace(cfg.SeedAdminEmail)), hash)
	return err
}

func buildStorage(ctx context.Context, cfg config.Config) (storage.Storage, error) {
	if cfg.StorageDriver == "s3" {
		return storage.NewS3(ctx, cfg.S3Endpoint, cfg.S3Region, cfg.S3Bucket, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3ForcePathStyle)
	}
	return storage.NewLocal(cfg.LocalStoragePath)
}
