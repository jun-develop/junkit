package middleware

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: limiter_internal_test.go
// Purpose: Verify the limiter's client store internals, alert throttling and alert HTML escaping.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 5
//
// #  Test Case                                       Scenario                               Expected Result
// 1  TestClientStore_GetClientIsStablePerKey         Same key twice, then another key       Same entry for the same key, a new entry for another key
// 2  TestNewClientStore_Defaults                     Empty LimiterOptions                   64 shards, 1m cleanup interval, 10m client TTL
// 3  TestClientStore_SweepEvictsIdleEntriesOnAccess  Entry idle for an hour, sweep overdue  The next access in the shard evicts the idle entry
// 4  TestClient_ShouldAlertThrottles                 Alerts at 0s, 1s and 2m with a 1m TTL  true, false, true
// 5  TestSendAlert_EscapesRequestValues              Request path containing HTML           One alert, path HTML-escaped, title markup kept
//
// =============================================================================

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/notification"
)

// recordingNotifier is a notification.Notifier that records Send messages and ignores error reports.
type recordingNotifier struct{ messages []string }

func (n *recordingNotifier) Send(_ context.Context, _ notification.Level, msg string) {
	n.messages = append(n.messages, msg)
}

func (n *recordingNotifier) ReportError(context.Context, *http.Request, notification.ErrorReport) {}

func TestClientStore_GetClientIsStablePerKey(t *testing.T) {
	store := NewClientStore(LimiterOptions{RequestsPerSecond: 5, Burst: 5})
	first := store.getClient("203.0.113.1")

	if store.getClient("203.0.113.1") != first {
		t.Fatal("getClient() returned a different entry for the same key on a second call")
	}

	if store.getClient("203.0.113.2") == first {
		t.Fatal("getClient() returned the same entry for two different keys")
	}
}

func TestNewClientStore_Defaults(t *testing.T) {
	store := NewClientStore(LimiterOptions{})

	if len(store.shards) != 64 {
		t.Errorf("shard count = %d, want 64", len(store.shards))
	}

	if store.cleanupInterval != time.Minute {
		t.Errorf("cleanupInterval = %v, want 1m", store.cleanupInterval)
	}

	if store.clientTTL != 10*time.Minute {
		t.Errorf("clientTTL = %v, want 10m", store.clientTTL)
	}
}

func TestClientStore_SweepEvictsIdleEntriesOnAccess(t *testing.T) {
	store := NewClientStore(LimiterOptions{RequestsPerSecond: 5, Burst: 5, NumShards: 1})
	old := store.getClient("203.0.113.1")
	old.lastSeen.Store(time.Now().Add(-time.Hour).UnixNano())
	store.shards[0].lastSweep.Store(time.Now().Add(-time.Hour).UnixNano())

	store.getClient("203.0.113.2")

	if _, exists := store.shards[0].clients["203.0.113.1"]; exists {
		t.Fatal("an entry idle past ClientTTL survived the sweep run by its shard's next request")
	}
}

func TestClient_ShouldAlertThrottles(t *testing.T) {
	var c client
	now := time.Now()

	if !c.shouldAlert(now, time.Minute) {
		t.Fatal("first shouldAlert() = false, want true")
	}

	if c.shouldAlert(now.Add(time.Second), time.Minute) {
		t.Fatal("shouldAlert() 1s later = true, want false within the TTL")
	}

	if !c.shouldAlert(now.Add(2*time.Minute), time.Minute) {
		t.Fatal("shouldAlert() 2m later = false, want true after the TTL")
	}
}

func TestSendAlert_EscapesRequestValues(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/%3Cb%3Ex&y", nil)
	n := &recordingNotifier{}

	sendAlert(c, n, "<b>Title</b>", "IP", "1.2.3.4")

	if len(n.messages) != 1 {
		t.Fatalf("got %d alerts, want 1: %v", len(n.messages), n.messages)
	}

	msg := n.messages[0]
	if strings.Contains(msg, "<b>x") {
		t.Errorf("alert = %q, want the raw path markup escaped", msg)
	}

	if !strings.Contains(msg, "/&lt;b&gt;x&amp;y") {
		t.Errorf("alert = %q, want the escaped path /&lt;b&gt;x&amp;y", msg)
	}

	if !strings.Contains(msg, "<b>Title</b>") {
		t.Errorf("alert = %q, want the title markup kept", msg)
	}
}
