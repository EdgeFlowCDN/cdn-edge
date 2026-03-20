package purge

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const purgeChannel = "cdn:purge"

// Command represents a cache purge command.
type Command struct {
	Type    string   `json:"type"`    // url, dir, all
	Targets []string `json:"targets"`
	Domain  string   `json:"domain"`
}

// PurgeFunc is called when a purge command is received.
type PurgeFunc func(cmd Command)

// Subscriber listens for purge commands on Redis pub/sub.
type Subscriber struct {
	client  *redis.Client
	onPurge PurgeFunc
	stopCh  chan struct{}
}

// NewSubscriber creates a Redis purge subscriber.
func NewSubscriber(addr, password string, db int, onPurge PurgeFunc) *Subscriber {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &Subscriber{
		client:  client,
		onPurge: onPurge,
		stopCh:  make(chan struct{}),
	}
}

// Start begins listening for purge commands.
func (s *Subscriber) Start() {
	go s.subscribeLoop()
}

// Stop stops the subscriber.
func (s *Subscriber) Stop() {
	close(s.stopCh)
	s.client.Close()
}

func (s *Subscriber) subscribeLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		if err := s.subscribe(); err != nil {
			log.Printf("[redis-purge] subscribe error: %v, retrying in 3s", err)
			select {
			case <-s.stopCh:
				return
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func (s *Subscriber) subscribe() error {
	ctx := context.Background()
	pubsub := s.client.Subscribe(ctx, purgeChannel)
	defer pubsub.Close()

	log.Printf("[redis-purge] subscribed to %s", purgeChannel)

	ch := pubsub.Channel()
	for {
		select {
		case <-s.stopCh:
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			var cmd Command
			if err := json.Unmarshal([]byte(msg.Payload), &cmd); err != nil {
				log.Printf("[redis-purge] bad message: %v", err)
				continue
			}
			log.Printf("[redis-purge] received purge: type=%s domain=%s targets=%d", cmd.Type, cmd.Domain, len(cmd.Targets))
			if s.onPurge != nil {
				s.onPurge(cmd)
			}
		}
	}
}

// Publisher publishes purge commands to Redis (used by control plane).
type Publisher struct {
	client *redis.Client
}

// NewPublisher creates a Redis purge publisher.
func NewPublisher(addr, password string, db int) *Publisher {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &Publisher{client: client}
}

// Publish sends a purge command to all subscribers.
func (p *Publisher) Publish(cmd Command) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	return p.client.Publish(context.Background(), purgeChannel, data).Err()
}

// Close closes the publisher.
func (p *Publisher) Close() {
	p.client.Close()
}
