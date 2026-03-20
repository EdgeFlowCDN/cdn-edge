package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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

	// Handle shutdown signals
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		cdnlog.Info("shutting down", "signal", sig.String())
		os.Exit(0)
	}()

	cdnlog.Info("EdgeFlow edge node starting",
		"listen", cfg.Server.Listen,
		"domains", len(cfg.Domains),
		"memory_cache", cfg.Cache.Memory.MaxSize,
		"disk_cache", cfg.Cache.Disk.MaxSize,
	)

	if err := server.ListenAndServe(); err != nil {
		cdnlog.Fatal("server failed", "error", err)
	}
}
