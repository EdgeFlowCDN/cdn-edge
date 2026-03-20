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

	// Start server
	server := proxy.NewServer(cfg, cacheManager, accessLogger)

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
		"domains", len(cfg.Domains),
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
