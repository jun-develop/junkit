package cache

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: memory_internal_test.go
// Purpose: Verify that Memory sweeps expired entries on writes, at most once per sweepInterval.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 2
//
// #  Test Case                               Scenario                                   Expected Result
// 1  TestMemory_SweepsExpiredEntriesOnWrite  Last sweep older than sweepInterval        Write deletes expired entries, keeps live ones
// 2  TestMemory_SweepsExpiredEntriesOnWrite  Last sweep more recent than sweepInterval  Write leaves expired entries in place
//
// =============================================================================

import (
	"context"
	"testing"
	"time"
)

func TestMemory_SweepsExpiredEntriesOnWrite(t *testing.T) {
	tests := []struct {
		name          string
		lastSweepAgo  time.Duration
		wantExpiredIn bool
	}{
		{"last sweep older than sweepInterval", 2 * sweepInterval, false},
		{"last sweep more recent than sweepInterval", sweepInterval / 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			c := NewMemory[string](MemoryOptions{})
			c.lastSweep = now.Add(-tt.lastSweepAgo)
			c.items["expired"] = entry[string]{value: "old", expireAt: now.Add(-time.Second)}
			c.items["live"] = entry[string]{value: "fresh", expireAt: now.Add(time.Hour)}

			if err := c.Set(context.Background(), "new", "v", time.Minute); err != nil {
				t.Fatalf("Set() error = %v", err)
			}

			if _, ok := c.items["expired"]; ok != tt.wantExpiredIn {
				t.Errorf("expired entry still stored = %v, want %v", ok, tt.wantExpiredIn)
			}

			if _, ok := c.items["live"]; !ok {
				t.Error("live entry was removed, want it kept")
			}
		})
	}
}
