package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DomainConfig mirrors the control plane's domain config.
type DomainConfig struct {
	Host    string         `json:"host"`
	Origins []OriginConfig `json:"origins"`
	Cache   CacheConfig    `json:"cache"`
}

type OriginConfig struct {
	Addr     string `json:"addr"`
	Weight   int    `json:"weight"`
	Priority int    `json:"priority"`
}

type CacheConfig struct {
	DefaultTTL  string `json:"default_ttl"`
	IgnoreQuery bool   `json:"ignore_query"`
	ForceTTL    string `json:"force_ttl"`
}

// ConfigUpdateCallback is called when configuration changes are received.
type ConfigUpdateCallback func(domains []DomainConfig)

// PurgeCallback is called when a purge command is received.
type PurgeCallback func(purgeType string, targets []string, domain string)

// Client connects to the control plane gRPC server.
type Client struct {
	addr       string
	nodeID     string
	nodeIP     string
	conn       *grpc.ClientConn
	onConfig   ConfigUpdateCallback
	onPurge    PurgeCallback
	stopCh     chan struct{}
}

// NewClient creates a new gRPC client for the control plane.
func NewClient(addr, nodeID, nodeIP string, onConfig ConfigUpdateCallback, onPurge PurgeCallback) *Client {
	return &Client{
		addr:     addr,
		nodeID:   nodeID,
		nodeIP:   nodeIP,
		onConfig: onConfig,
		onPurge:  onPurge,
		stopCh:   make(chan struct{}),
	}
}

// Start connects to the control plane and begins watching for config updates.
func (c *Client) Start() error {
	conn, err := grpc.NewClient(c.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to control plane: %w", err)
	}
	c.conn = conn

	// Pull full config on startup
	if err := c.pullFullConfig(); err != nil {
		log.Printf("[grpc-client] failed to pull full config: %v (will retry via watch)", err)
	}

	// Start watching for updates
	go c.watchLoop()

	// Start heartbeat
	go c.heartbeatLoop()

	return nil
}

// Stop disconnects from the control plane.
func (c *Client) Stop() {
	close(c.stopCh)
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *Client) pullFullConfig() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &NodeInfo{NodeId: c.nodeID, Ip: c.nodeIP}
	reqBytes, _ := json.Marshal(req)

	var respBytes []byte
	err := c.conn.Invoke(ctx, "/edgeflow.EdgeService/GetFullConfig", reqBytes, &respBytes)
	if err != nil {
		return err
	}

	var resp FullConfigResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return err
	}

	var domains []DomainConfig
	if err := json.Unmarshal([]byte(resp.ConfigJSON), &domains); err != nil {
		return err
	}

	if c.onConfig != nil {
		c.onConfig(domains)
	}
	log.Printf("[grpc-client] pulled full config: %d domains, version %d", len(domains), resp.Version)
	return nil
}

func (c *Client) watchLoop() {
	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		if err := c.watch(); err != nil {
			log.Printf("[grpc-client] watch error: %v, reconnecting in 5s", err)
			select {
			case <-c.stopCh:
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (c *Client) watch() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := c.conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true},
		"/edgeflow.EdgeService/WatchConfig")
	if err != nil {
		return err
	}

	req := &NodeInfo{NodeId: c.nodeID, Ip: c.nodeIP}
	reqBytes, _ := json.Marshal(req)
	if err := stream.SendMsg(reqBytes); err != nil {
		return err
	}
	if err := stream.CloseSend(); err != nil {
		return err
	}

	log.Printf("[grpc-client] watching for config updates")

	for {
		var respBytes []byte
		if err := stream.RecvMsg(&respBytes); err != nil {
			return err
		}

		var update ConfigUpdate
		if err := json.Unmarshal(respBytes, &update); err != nil {
			log.Printf("[grpc-client] bad update: %v", err)
			continue
		}

		switch update.Action {
		case "full":
			if c.onConfig != nil {
				c.onConfig(update.Domains)
			}
		case "purge":
			if c.onPurge != nil && update.Purge != nil {
				c.onPurge(update.Purge.Type, update.Purge.Targets, update.Purge.Domain)
			}
		}
	}
}

func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.sendHeartbeat()
		}
	}
}

func (c *Client) sendHeartbeat() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &HeartbeatRequest{
		NodeId:    c.nodeID,
		Timestamp: time.Now().Unix(),
	}
	reqBytes, _ := json.Marshal(req)

	var respBytes []byte
	if err := c.conn.Invoke(ctx, "/edgeflow.EdgeService/Heartbeat", reqBytes, &respBytes); err != nil {
		log.Printf("[grpc-client] heartbeat failed: %v", err)
	}
}

// Types matching control plane proto

type NodeInfo struct {
	NodeId        string `json:"node_id"`
	Ip            string `json:"ip"`
	ConfigVersion int64  `json:"config_version"`
}

type FullConfigResponse struct {
	Version    int64  `json:"version"`
	ConfigJSON string `json:"config_json"`
}

type ConfigUpdate struct {
	Action  string         `json:"action"`
	Domains []DomainConfig `json:"domains,omitempty"`
	Purge   *PurgeCommand  `json:"purge,omitempty"`
	Version int64          `json:"version"`
}

type PurgeCommand struct {
	TaskID  int64    `json:"task_id"`
	Type    string   `json:"type"`
	Targets []string `json:"targets"`
	Domain  string   `json:"domain"`
}

type HeartbeatRequest struct {
	NodeId       string  `json:"node_id"`
	Timestamp    int64   `json:"timestamp"`
	CpuUsage     float64 `json:"cpu_usage"`
	MemUsage     float64 `json:"mem_usage"`
	BandwidthBps int64   `json:"bandwidth_bps"`
	Connections  int64   `json:"connections"`
}
