package metrics

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestStreamLatencyRealTime(t *testing.T) {
	s := NewStreamLatency()
	s.Insert(10)
	s.Insert(20)
	if p50 := s.P50(); p50 < 10 || p50 > 20 {
		t.Fatalf("after two samples p50=%d", p50)
	}

	s.Insert(1000)
	s.Insert(1000)
	p95 := s.P95()
	if p95 < 500 {
		t.Fatalf("p95 should rise after large inserts, got %d", p95)
	}
}

func TestLatencyRegistryNoHandlerLock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewLatencyRegistry(256, nil)
	reg.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := uint32(0); j < 50; j++ {
				reg.Record("eng", 10+j)
			}
		}()
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for reg.P50("eng") == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if reg.P50("eng") == 0 {
		t.Fatal("expected non-zero p50 after ingest")
	}
	if reg.P95("eng") == 0 {
		t.Fatal("expected non-zero p95 after ingest")
	}
}

func TestLatencyRegistryResetTeam(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewLatencyRegistry(64, nil)
	reg.Start(ctx)
	reg.Record("data", 500)
	time.Sleep(20 * time.Millisecond)
	if reg.P50("data") == 0 {
		t.Fatal("expected sample ingested")
	}

	reg.ResetTeam("data")
	if reg.Count("data") != 0 {
		t.Fatalf("count after reset=%d", reg.Count("data"))
	}
}
