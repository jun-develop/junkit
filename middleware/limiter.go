package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/jun-develop/junkit/notification"
	"github.com/jun-develop/junkit/response"
	"github.com/jun-develop/junkit/security"
)

// LimiterOptions configures rate limiting rules for a client (an IP, or an IPv6 /64; see
// security.ClientKey).
type LimiterOptions struct {
	// RequestsPerSecond is the sustained rate allowed per client IP.
	RequestsPerSecond int

	// Burst is the maximum burst size allowed above RequestsPerSecond.
	Burst int

	// CleanupInterval is how often a shard's idle client entries are swept, on its next request.
	// Defaults to one minute.
	CleanupInterval time.Duration

	// ClientTTL is how long a client entry survives with no requests before eviction. Defaults to
	// ten minutes.
	ClientTTL time.Duration

	// LogTTL throttles repeated "rate limit exceeded" log/alert lines to at most one per client per
	// interval. Defaults to one minute.
	LogTTL time.Duration

	// NumShards controls lock striping across the client map. Defaults to 64.
	NumShards int

	// IPWhitelist lists client IPs that always bypass the limiter.
	IPWhitelist []string
}

// withDefaults fills unset fields.
func (o LimiterOptions) withDefaults() LimiterOptions {
	if o.CleanupInterval <= 0 {
		o.CleanupInterval = time.Minute
	}

	if o.ClientTTL <= 0 {
		o.ClientTTL = 10 * time.Minute
	}

	if o.LogTTL <= 0 {
		o.LogTTL = time.Minute
	}

	if o.NumShards <= 0 {
		o.NumShards = 64
	}

	return o
}

// client tracks one visitor's rate limiter, last-seen time and last alert time (Unix nanoseconds).
type client struct {
	limiter   *rate.Limiter
	lastSeen  atomic.Int64
	lastAlert atomic.Int64
}

// shard represents a lock-striped subset of rate limiters to reduce lock contention.
type shard struct {
	mu        sync.RWMutex
	clients   map[string]*client
	lastSweep atomic.Int64
}

// ClientStore manages per-client rate limiters across sharded maps. Idle entries are evicted lazily
// by the requests that reach their shard, so no background goroutine is needed.
type ClientStore struct {
	shards          []*shard
	cleanupInterval time.Duration
	clientTTL       time.Duration
	rps             int
	burst           int
}

// NewClientStore creates a ClientStore configured by opts.
func NewClientStore(opts LimiterOptions) *ClientStore {
	opts = opts.withDefaults()
	s := &ClientStore{
		shards:          make([]*shard, opts.NumShards),
		cleanupInterval: opts.CleanupInterval,
		clientTTL:       opts.ClientTTL,
		rps:             opts.RequestsPerSecond,
		burst:           opts.Burst,
	}

	now := time.Now().UnixNano()
	for i := range s.shards {
		s.shards[i] = &shard{clients: make(map[string]*client)}
		s.shards[i].lastSweep.Store(now)
	}

	return s
}

// getShard selects key's shard via an FNV-1a hash.
func (s *ClientStore) getShard(key string) *shard {
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}

	return s.shards[hash%uint32(len(s.shards))]
}

// getClient returns or creates the client entry for key, first sweeping its shard when due.
func (s *ClientStore) getClient(key string) *client {
	sh := s.getShard(key)
	now := time.Now()
	s.sweepIfDue(sh, now)

	sh.mu.RLock()
	c, exists := sh.clients[key]
	sh.mu.RUnlock()

	if exists {
		if now.Sub(time.Unix(0, c.lastSeen.Load())) > 10*time.Second {
			c.lastSeen.Store(now.UnixNano())
		}

		return c
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	if c, exists := sh.clients[key]; exists {
		return c
	}

	c = &client{limiter: rate.NewLimiter(rate.Limit(s.rps), s.burst)}
	c.lastSeen.Store(now.UnixNano())
	sh.clients[key] = c

	return c
}

// sweepIfDue evicts sh's idle entries once per cleanupInterval; the CAS lets one caller do it.
func (s *ClientStore) sweepIfDue(sh *shard, now time.Time) {
	last := sh.lastSweep.Load()
	if now.Sub(time.Unix(0, last)) < s.cleanupInterval || !sh.lastSweep.CompareAndSwap(last, now.UnixNano()) {
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	for key, c := range sh.clients {
		if now.Sub(time.Unix(0, c.lastSeen.Load())) > s.clientTTL {
			delete(sh.clients, key)
		}
	}
}

// shouldAlert reports whether a log or alert may be emitted for c, recording the time when it may.
func (c *client) shouldAlert(now time.Time, ttl time.Duration) bool {
	last := c.lastAlert.Load()
	if last != 0 && now.Sub(time.Unix(0, last)) < ttl {
		return false
	}

	return c.lastAlert.CompareAndSwap(last, now.UnixNano())
}

// Limiter creates a Gin middleware that rate-limits incoming HTTP requests per client IP. It panics
// when RequestsPerSecond or Burst is not positive. Clients are keyed by c.ClientIP(), so the
// engine's trusted proxies must be set correctly.
//
// Usage:
//
//	router.Use(middleware.Limiter(middleware.LimiterOptions{
//	    RequestsPerSecond: 10, Burst: 20,
//	}, zapLogger, responder, notifier))
func Limiter(opts LimiterOptions, logger *zap.Logger, responder response.Responder, notifier notification.Notifier) gin.HandlerFunc {
	if opts.RequestsPerSecond <= 0 || opts.Burst <= 0 {
		panic(fmt.Sprintf("middleware: Limiter requires RequestsPerSecond and Burst > 0, got %d and %d", opts.RequestsPerSecond, opts.Burst))
	}

	opts = opts.withDefaults()
	store := NewClientStore(opts)
	whitelist := toSet(opts.IPWhitelist)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		if _, ok := whitelist[ip]; ok {
			c.Next()
			return
		}

		// Keyed by ClientKey: an IPv6 client rotating addresses inside its /64 shares one bucket.
		entry := store.getClient(security.ClientKey(ip))
		if entry.limiter.Allow() {
			c.Next()
			return
		}

		if entry.shouldAlert(time.Now(), opts.LogTTL) {
			logger.Warn("middleware: rate limit exceeded",
				zap.String("ip", ip),
				zap.String("path", c.Request.URL.Path),
				zap.Int("limit_rps", opts.RequestsPerSecond),
			)

			sendAlert(c, notifier, "🚦 <b>Rate Limit Exceeded</b>", "IP", ip)
		}

		responder.Send(c, http.StatusTooManyRequests, "too_many_requests")
		c.Abort()
	}
}

// toSet returns the items as a set.
func toSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}

	return set
}
