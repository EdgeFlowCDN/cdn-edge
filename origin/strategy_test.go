package origin

import (
	"testing"

	"github.com/EdgeFlowCDN/cdn-edge/config"
)

var testOrigins = []config.OriginConfig{
	{Addr: "https://origin1.example.com", Weight: 70, Priority: 0},
	{Addr: "https://origin2.example.com", Weight: 30, Priority: 1},
}

func TestRoundRobin(t *testing.T) {
	rr := &RoundRobin{}

	seen := map[string]int{}
	for i := 0; i < 10; i++ {
		o := rr.Select(testOrigins, 0)
		seen[o.Addr]++
	}

	if len(seen) != 2 {
		t.Errorf("expected 2 unique origins, got %d", len(seen))
	}
}

func TestWeighted(t *testing.T) {
	w := &Weighted{}

	counts := map[string]int{}
	n := 10000
	for i := 0; i < n; i++ {
		o := w.Select(testOrigins, 0)
		counts[o.Addr]++
	}

	// With weights 70/30, origin1 should get roughly 70%
	ratio := float64(counts["https://origin1.example.com"]) / float64(n)
	if ratio < 0.6 || ratio > 0.8 {
		t.Errorf("weighted ratio = %.2f, expected ~0.70", ratio)
	}
}

func TestPrimaryBackup(t *testing.T) {
	pb := &PrimaryBackup{}

	// attempt 0 should return primary (priority 0)
	o := pb.Select(testOrigins, 0)
	if o.Addr != "https://origin1.example.com" {
		t.Errorf("attempt 0: got %s, want origin1", o.Addr)
	}

	// attempt 1 should return backup (priority 1)
	o = pb.Select(testOrigins, 1)
	if o.Addr != "https://origin2.example.com" {
		t.Errorf("attempt 1: got %s, want origin2", o.Addr)
	}

	// attempt beyond range should return last
	o = pb.Select(testOrigins, 5)
	if o.Addr != "https://origin2.example.com" {
		t.Errorf("attempt 5: got %s, want origin2", o.Addr)
	}
}

func TestNewStrategy(t *testing.T) {
	if _, ok := NewStrategy("round-robin").(*RoundRobin); !ok {
		t.Error("expected RoundRobin")
	}
	if _, ok := NewStrategy("weighted").(*Weighted); !ok {
		t.Error("expected Weighted")
	}
	if _, ok := NewStrategy("primary-backup").(*PrimaryBackup); !ok {
		t.Error("expected PrimaryBackup")
	}
	if _, ok := NewStrategy("unknown").(*RoundRobin); !ok {
		t.Error("expected default RoundRobin")
	}
}
