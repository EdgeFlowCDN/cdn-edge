package log

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS cdn_access_log (
    time          DateTime,
    client_ip     String,
    method        LowCardinality(String),
    host          String,
    uri           String,
    status        UInt16,
    body_bytes    UInt64,
    cache_status  LowCardinality(String),
    upstream_status UInt16,
    upstream_time Float64,
    request_time  Float64,
    referer       String,
    user_agent    String,
    request_id    String
) ENGINE = MergeTree()
ORDER BY (host, time)
PARTITION BY toYYYYMM(time)
TTL time + INTERVAL 90 DAY
`

// ClickHouseShipper batches access log entries and ships them to ClickHouse.
type ClickHouseShipper struct {
	conn      driver.Conn
	mu        sync.Mutex
	buffer    []AccessEntry
	batchSize int
	flushInterval time.Duration
	stopCh    chan struct{}
}

// NewClickHouseShipper creates a new log shipper.
func NewClickHouseShipper(addr, database string, batchSize int, flushInterval time.Duration) (*ClickHouseShipper, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: database,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, err
	}

	// Create table if not exists
	if err := conn.Exec(context.Background(), createTableSQL); err != nil {
		return nil, err
	}

	s := &ClickHouseShipper{
		conn:          conn,
		buffer:        make([]AccessEntry, 0, batchSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		stopCh:        make(chan struct{}),
	}

	go s.flushLoop()

	log.Printf("[clickhouse] connected to %s, database=%s", addr, database)
	return s, nil
}

// Ship adds an access log entry to the buffer.
func (s *ClickHouseShipper) Ship(entry AccessEntry) {
	s.mu.Lock()
	s.buffer = append(s.buffer, entry)
	shouldFlush := len(s.buffer) >= s.batchSize
	s.mu.Unlock()

	if shouldFlush {
		go s.flush()
	}
}

// Stop flushes remaining entries and closes the connection.
func (s *ClickHouseShipper) Stop() {
	close(s.stopCh)
	s.flush()
	s.conn.Close()
}

func (s *ClickHouseShipper) flushLoop() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

func (s *ClickHouseShipper) flush() {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return
	}
	entries := s.buffer
	s.buffer = make([]AccessEntry, 0, s.batchSize)
	s.mu.Unlock()

	ctx := context.Background()
	batch, err := s.conn.PrepareBatch(ctx,
		"INSERT INTO cdn_access_log (time, client_ip, method, host, uri, status, body_bytes, cache_status, upstream_status, upstream_time, request_time, referer, user_agent, request_id)")
	if err != nil {
		log.Printf("[clickhouse] prepare batch error: %v (dropping %d entries)", err, len(entries))
		return
	}

	for _, e := range entries {
		if err := batch.Append(
			e.Time, e.ClientIP, e.Method, e.Host, e.URI,
			uint16(e.Status), uint64(e.BodyBytes), e.CacheStatus,
			uint16(e.UpstreamStatus), e.UpstreamTime, e.RequestTime,
			e.Referer, e.UserAgent, e.RequestID,
		); err != nil {
			log.Printf("[clickhouse] append error: %v", err)
		}
	}

	if err := batch.Send(); err != nil {
		log.Printf("[clickhouse] send batch error: %v (dropped %d entries)", err, len(entries))
		return
	}

	log.Printf("[clickhouse] shipped %d log entries", len(entries))
}
