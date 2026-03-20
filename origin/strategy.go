package origin

import (
	"math/rand"
	"sort"
	"sync/atomic"

	"github.com/EdgeFlowCDN/cdn-edge/config"
)

// Strategy selects an origin server for a request.
type Strategy interface {
	Select(origins []config.OriginConfig, attempt int) *config.OriginConfig
}

// RoundRobin cycles through origins sequentially.
type RoundRobin struct {
	counter atomic.Uint64
}

func (r *RoundRobin) Select(origins []config.OriginConfig, attempt int) *config.OriginConfig {
	if len(origins) == 0 {
		return nil
	}
	idx := r.counter.Add(1) - 1
	return &origins[(int(idx)+attempt)%len(origins)]
}

// Weighted selects origins based on their weight values.
type Weighted struct{}

func (w *Weighted) Select(origins []config.OriginConfig, attempt int) *config.OriginConfig {
	if len(origins) == 0 {
		return nil
	}
	totalWeight := 0
	for _, o := range origins {
		totalWeight += o.Weight
	}
	if totalWeight <= 0 {
		return &origins[rand.Intn(len(origins))]
	}

	r := rand.Intn(totalWeight)
	for i := range origins {
		r -= origins[i].Weight
		if r < 0 {
			return &origins[i]
		}
	}
	return &origins[len(origins)-1]
}

// PrimaryBackup tries the primary origin first, then falls back to backups.
type PrimaryBackup struct{}

func (p *PrimaryBackup) Select(origins []config.OriginConfig, attempt int) *config.OriginConfig {
	if len(origins) == 0 {
		return nil
	}

	// Sort by priority (0=primary, higher=backup)
	sorted := make([]config.OriginConfig, len(origins))
	copy(sorted, origins)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	idx := attempt
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return &sorted[idx]
}

// NewStrategy creates a Strategy by name.
func NewStrategy(name string) Strategy {
	switch name {
	case "weighted":
		return &Weighted{}
	case "primary-backup":
		return &PrimaryBackup{}
	default:
		return &RoundRobin{}
	}
}
