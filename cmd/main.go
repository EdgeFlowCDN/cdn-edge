package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EdgeFlowCDN/cdn-edge/cache"
	"github.com/EdgeFlowCDN/cdn-edge/config"
	cdngrpc "github.com/EdgeFlowCDN/cdn-edge/grpc"
	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
	"github.com/EdgeFlowCDN/cdn-edge/proxy"
)

func main() {
	configPath := flag.String("config", "configs/edge-config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	if err := cdnlog.Init(cfg.Log.Level, cfg.Log.ErrorLog); err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer cdnlog.Sync()

	// Parse cache sizes
	memMaxSize, err := config.ParseSize(cfg.Cache.Memory.MaxSize)
	if err != nil {
		cdnlog.Fatal("invalid memory cache max_size", "error", err)
	}
	memMaxObj, err := config.ParseSize(cfg.Cache.Memory.MaxObjectSize)
	if err != nil {
		cdnlog.Fatal("invalid memory cache max_object_size", "error", err)
	}
	diskMaxSize, err := config.ParseSize(cfg.Cache.Disk.MaxSize)
	if err != nil {
		cdnlog.Fatal("invalid disk cache max_size", "error", err)
	}
	diskMaxObj, err := config.ParseSize(cfg.Cache.Disk.MaxObjectSize)
	if err != nil {
		cdnlog.Fatal("invalid disk cache max_object_size", "error", err)
	}

	// Initialize caches
	memCache := cache.NewMemoryCache(memMaxSize, memMaxObj)

	diskCache, err := cache.NewDiskCache(cfg.Cache.Disk.Path, diskMaxSize, diskMaxObj)
	if err != nil {
		cdnlog.Fatal("failed to init disk cache", "error", err)
	}
	defer diskCache.Stop()

	cacheManager := cache.NewManager(memCache, diskCache)

	// Initialize access logger
	accessLogger, err := cdnlog.NewAccessLogger(cfg.Log.AccessLog)
	if err != nil {
		cdnlog.Fatal("failed to init access logger", "error", err)
	}
	defer accessLogger.Close()

	// Start server (initially with YAML-configured domains)
	server := proxy.NewServer(cfg, cacheManager, accessLogger)

	// Connect to control plane for config sync (if configured)
	if cfg.ControlPlane.Addr != "" {
		cdnlog.Info("connecting to control plane",
			"addr", cfg.ControlPlane.Addr,
			"node_id", cfg.ControlPlane.NodeID,
		)

		grpcClient := cdngrpc.NewClient(
			cfg.ControlPlane.Addr,
			cfg.ControlPlane.NodeID,
			cfg.ControlPlane.NodeIP,
			// Config update callback — hot-reload domain configs
			func(domains []cdngrpc.DomainConfig) {
				edgeConfigs := cdngrpc.ToEdgeConfigs(domains)
				server.Reloader().ReloadDomains(edgeConfigs)
				cdnlog.Info("domain config reloaded from control plane", "domains", len(edgeConfigs))
			},
			// Purge callback — execute cache purge
			func(purgeType string, targets []string, domain string) {
				executePurge(server.CacheManager(), purgeType, targets, domain)
			},
		)

		if err := grpcClient.Start(); err != nil {
			cdnlog.Error("failed to connect to control plane, using local config", "error", err)
		} else {
			defer grpcClient.Stop()
			cdnlog.Info("connected to control plane")
		}
	} else {
		cdnlog.Info("no control plane configured, using local YAML config",
			"domains", len(cfg.Domains),
		)
	}

	// Start metrics/health server
	if cfg.Server.MetricsListen != "" {
		go func() {
			if err := server.StartMetricsServer(cfg.Server.MetricsListen); err != nil {
				cdnlog.Error("metrics server failed", "error", err)
			}
		}()
	}

	cdnlog.Info("EdgeFlow edge node starting",
		"listen", cfg.Server.Listen,
		"memory_cache", cfg.Cache.Memory.MaxSize,
		"disk_cache", cfg.Cache.Disk.MaxSize,
	)

	// Create HTTP server explicitly for graceful shutdown
	srv := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: server,
		ConnState: func(conn net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				server.Metrics().ConnOpen()
			case http.StateClosed, http.StateHijacked:
				server.Metrics().ConnClose()
			}
		},
	}

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		cdnlog.Info("received shutdown signal, draining connections", "signal", sig.String())

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			cdnlog.Error("graceful shutdown failed, forcing close", "error", err)
			srv.Close()
		} else {
			cdnlog.Info("graceful shutdown complete")
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		cdnlog.Fatal("server failed", "error", err)
	}
}

// executePurge handles purge commands from gRPC or Redis.
func executePurge(cm *cache.Manager, purgeType string, targets []string, domain string) {
	switch purgeType {
	case "url":
		for _, url := range targets {
			cm.Delete(url)
		}
		cdnlog.Info("purge URLs completed", "count", len(targets), "domain", domain)
	case "dir":
		total := 0
		for _, dir := range targets {
			total += cm.Purge(dir)
		}
		cdnlog.Info("purge directory completed", "removed", total, "domain", domain)
	case "all":
		prefix := "http://" + domain
		n1 := cm.Purge(prefix)
		prefix = "https://" + domain
		n2 := cm.Purge(prefix)
		cdnlog.Info("purge all completed", "removed", n1+n2, "domain", domain)
	default:
		cdnlog.Warn("unknown purge type", "type", purgeType)
	}

}
