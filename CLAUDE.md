# cdn-edge

EdgeFlow CDN edge node — high-performance HTTP/HTTPS caching reverse proxy.

## Tech Stack

Go 1.23, Prometheus, gRPC, Redis, ClickHouse

## Project Structure

```
cmd/          Entry point (main.go)
config/       YAML config loading and validation
cache/        Two-tier cache: sharded LRU memory + hash-bucketed disk
  cache.go        Cache interface and status types
  entry.go        CacheEntry struct and cache key generation
  memory.go       Sharded LRU memory cache (256 shards)
  disk.go         File-based disk cache with background eviction
  manager.go      Two-tier cache coordinator
  cachecontrol.go Cache-Control header parsing and TTL computation
proxy/        HTTP proxy and middleware
  proxy.go        Server struct, ListenAndServe, metrics server
  handler.go      Main ServeHTTP request flow
  metrics.go      Prometheus metrics (per-instance registry)
  https.go        TLS/SNI cert store, HTTPS server
  acme.go         Let's Encrypt auto-cert via autocert
  middleware.go   Rate limiter, anti-hotlinking, signed URL
  compress.go     Gzip/Brotli compression middleware
  range.go        HTTP Range request support
  reload.go       Thread-safe config hot-reload
origin/       Origin fetch logic
  fetcher.go      Singleflight coalescing, retry, timeout
  strategy.go     Round-robin, weighted, primary-backup
grpc/         gRPC client for control plane config sync
purge/        Redis pub/sub for cache purge broadcast
log/          Logging
  log.go          Zap structured logger
  access.go       JSON access log writer
  clickhouse.go   Async batched ClickHouse log shipper
configs/      YAML config files
```

## Key Design Decisions

- Cache key: `scheme://host/path?sorted_query`
- Memory cache: 256 shards with per-shard LRU, objects >1MB skip memory
- Disk cache: `hash[0:2]/hash[2:4]/hash.{data,meta}`, atomic writes via temp+rename
- Origin coalescing: singleflight pattern prevents thundering herd
- Metrics: per-instance Prometheus registry (avoids global state in tests)
- Compression: skips <1KB and already-compressed content types

## Running Tests

```bash
go test ./... -race -count=1
go test ./cache/... -bench=. -benchmem
```

## Performance

- Memory cache Get: ~9.5M ops/sec, 0 allocs
- Proxy cache hit: ~32K QPS
