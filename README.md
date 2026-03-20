# cdn-edge

EdgeFlow CDN edge node — high-performance HTTP/HTTPS reverse proxy with caching.

## Features

- HTTP/HTTPS reverse proxy with Host-based domain routing
- Two-tier LRU cache: sharded in-memory + hash-bucketed disk
- Cache-Control header parsing (s-maxage, max-age, Expires)
- Origin fetch with singleflight request coalescing and retry
- Origin strategies: round-robin, weighted, primary-backup
- HTTPS with SNI support (TLS 1.2+, HTTP/2)
- Let's Encrypt / ACME auto certificate management
- Per-IP token bucket rate limiting
- Anti-hotlinking (Referer whitelist + signed URL)
- Prometheus metrics (`/metrics`) and health check (`/health`)
- Structured JSON access logging
- ClickHouse log shipping (async batched)
- gRPC client for control plane config sync
- Redis pub/sub for cache purge broadcast

## Quick Start

```bash
# Build
go build -o bin/cdn-edge ./cmd

# Run
./bin/cdn-edge -config configs/edge-config.yaml

# Docker
docker build -t cdn-edge .
docker run -p 8080:8080 cdn-edge
```

## Configuration

See `configs/edge-config.yaml` for all options.

## Performance

- Memory cache Get: ~9.5M ops/sec (zero allocs)
- Full proxy cache hit: ~32K QPS (~37μs/op)
- Target: >10K QPS, P99 <5ms on cache hit

## Testing

```bash
go test ./... -race -count=1
go test ./cache/... -bench=. -benchmem
```
