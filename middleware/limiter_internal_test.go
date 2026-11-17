package middleware

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

func TestClientStore_GetClientIsStablePerKey(t *testing.T) {
	store := NewClientStore(LimiterOptions{RequestsPerSecond: 5, Burst: 5})
	first := store.getClient("203.0.113.1")

	if store.getClient("203.0.113.1") != first {
		t.Fatal("getClient returned a different entry for the same key on a second call")
	}

	if store.getClient("203.0.113.2") == first {
		t.Fatal("getClient returned the same entry for two different keys")
	}
}

func TestNewClientStore_Defaults(t *testing.T) {
	store := NewClientStore(LimiterOptions{})

	if len(store.shards) != 64 || store.cleanupInterval != time.Minute || store.clientTTL != 10*time.Minute {
		t.Fatalf("shards %d, cleanup %v, ttl %v, want 64, 1m, 10m", len(store.shards), store.cleanupInterval, store.clientTTL)
	}
}

func TestClientStore_SweepEvictsIdleEntriesOnAccess(t *testing.T) {
	store := NewClientStore(LimiterOptions{RequestsPerSecond: 5, Burst: 5, NumShards: 1})
	old := store.getClient("203.0.113.1")
	old.lastSeen.Store(time.Now().Add(-time.Hour).UnixNano())
	store.shards[0].lastSweep.Store(time.Now().Add(-time.Hour).UnixNano())

	store.getClient("203.0.113.2")

	if _, exists := store.shards[0].clients["203.0.113.1"]; exists {
		t.Fatal("an entry idle past ClientTTL survived the sweep its shard's next request ran")
	}
}

func TestClient_ShouldAlertThrottles(t *testing.T) {
	var c client
	now := time.Now()

	if !c.shouldAlert(now, time.Minute) || c.shouldAlert(now.Add(time.Second), time.Minute) ||
		!c.shouldAlert(now.Add(2*time.Minute), time.Minute) {
		t.Fatal("shouldAlert want true, false within the TTL, true after it")
	}
}

// recordingNotifier records Send messages.
type recordingNotifier struct{ messages []string }

// Send implements notification.Notifier.
func (n *recordingNotifier) Send(_ context.Context, _ notification.Level, msg string) {
	n.messages = append(n.messages, msg)
}

// ReportError implements notification.Notifier.
func (n *recordingNotifier) ReportError(context.Context, *http.Request, notification.ErrorReport) {}

func TestSendAlert_EscapesRequestValues(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/%3Cb%3Ex&y", nil)
	n := &recordingNotifier{}

	sendAlert(c, n, "<b>Title</b>", "IP", "1.2.3.4")

	if len(n.messages) != 1 || strings.Contains(n.messages[0], "<b>x") || !strings.Contains(n.messages[0], "/&lt;b&gt;x&amp;y") ||
		!strings.Contains(n.messages[0], "<b>Title</b>") {
		t.Fatalf("alert not built or path not HTML-escaped:\n%v", n.messages)
	}
}
