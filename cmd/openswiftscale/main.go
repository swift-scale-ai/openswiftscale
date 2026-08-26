package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/auth"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/config"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/gateway"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/provider"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/router"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/secret"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/store"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		response, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://127.0.0.1:8080/healthz")
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o750); err != nil {
		logger.Error("create data directory", "error", err)
		os.Exit(1)
	}
	modelCatalog, err := catalog.Load(cfg.CatalogPath)
	if err != nil {
		logger.Error("load model catalog", "error", err)
		os.Exit(1)
	}
	protector, err := secret.LoadOrCreate(cfg.MasterKeyPath, cfg.MasterKey)
	if err != nil {
		logger.Error("load master encryption key", "error", err)
		os.Exit(1)
	}
	database, err := store.OpenWithProtector(cfg.DatabasePath, protector)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	if err := database.SeedCatalog(context.Background(), modelCatalog); err != nil {
		logger.Error("seed provider catalog", "error", err)
		os.Exit(1)
	}
	for _, item := range modelCatalog.Providers {
		connection, found, loadErr := database.Provider(context.Background(), item.ID)
		if loadErr != nil || !found {
			logger.Error("load provider connection", "provider", item.ID, "error", loadErr)
			os.Exit(1)
		}
		providerConfig := connection.Provider
		if value := strings.TrimSpace(os.Getenv("OPENSWIFTSCALE_" + strings.ToUpper(item.ID) + "_BASE_URL")); value != "" {
			providerConfig.BaseURL = strings.TrimRight(value, "/")
		}
		legacyKey := config.ProviderKeyEnv(item.ID)
		var keyUpdate *string
		if legacyKey != "" && !connection.HasAPIKey {
			keyUpdate = &legacyKey
		}
		if providerConfig.BaseURL != connection.BaseURL || keyUpdate != nil {
			if err := database.SaveConnection(context.Background(), store.ConnectionUpdate{Provider: providerConfig, Name: connection.Name, Official: true, Enabled: connection.Enabled, APIKey: keyUpdate}); err != nil {
				logger.Error("import legacy provider configuration", "provider", item.ID, "error", err)
				os.Exit(1)
			}
		}
	}
	runtimeCatalog, keys, err := database.RuntimeCatalog(context.Background())
	if err != nil {
		logger.Error("load runtime provider configuration", "error", err)
		os.Exit(1)
	}
	routingRules, err := database.RoutingRules(context.Background())
	if err != nil {
		logger.Error("load routing rules", "error", err)
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: cfg.RequestTimeout, Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, MaxIdleConns: 100, MaxIdleConnsPerHost: 20,
		IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: 60 * time.Second,
	}}
	gateway.Version = version
	app := gateway.New(cfg, runtimeCatalog, auth.New(cfg.APIKeys, cfg.AuthDisabled), router.New(runtimeCatalog, keys, routingRules), provider.NewClient(httpClient), database, logger)
	server := &http.Server{Addr: cfg.ListenAddr, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}

	errors := make(chan error, 1)
	go func() {
		logger.Info("OpenSwiftScale started", "version", version, "address", cfg.ListenAddr)
		errors <- server.ListenAndServe()
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		logger.Info("shutdown requested")
	case err := <-errors:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped", "error", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown", "error", err)
	}
}
