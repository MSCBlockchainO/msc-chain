package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

type rpcLimiterEntry struct {
	// `limiter` stores the value associated with this record.
	limiter *rate.Limiter
	// `lastSeen` stores the value associated with this record.
	lastSeen time.Time
}

var (
	// `rpcLimiterMu` stores the synchronization state protecting shared data.
	rpcLimiterMu sync.Mutex
	// `rpcLimiters` stores the value used by this operation.
	rpcLimiters = make(map[string]rpcLimiterEntry)
	// `rpcLimiterCleanEvery` stores the value used by this operation.
	rpcLimiterCleanEvery uint64
	// `rpcInFlight` stores the value used by this operation.
	rpcInFlight int64
	// `rpcLocalLoadTestToken` authorizes loopback-only transaction load tests.
	rpcLocalLoadTestToken = strings.TrimSpace(os.Getenv("MSC_LOCAL_LOADTEST_TOKEN"))
)

type rpcStatusRecorder struct {
	// `ResponseWriter` stores the response produced by this operation.
	http.ResponseWriter
	// `status` stores the value associated with this record.
	status int
}

// WriteHeader writes header.
func (w *rpcStatusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

// Write implements the write helper.
func (w *rpcStatusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

// rpcTimeout implements the rpc timeout helper.
func rpcTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// rpcRequestClass implements the rpc request class helper.
func rpcRequestClass(r *http.Request) string {
	if r == nil {
		return "read"
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return "read"
	}
	return "write"
}

// rpcClientKey implements the rpc client key helper.
func rpcClientKey(r *http.Request, class string) string {
	// `host` stores the value produced by this operation.
	host := ""
	if r != nil {
		host, _, _ = net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
		if host == "" {
			host = strings.TrimSpace(r.RemoteAddr)
		}
	}
	if host == "" {
		host = "unknown"
	}
	return host + ":" + class
}

// rpcLimiterConfig implements the rpc limiter config helper.
func rpcLimiterConfig(class string) int {
	if class == "write" {
		return ConfigRPCWriteRateLimitPerMinute
	}
	return ConfigRPCReadRateLimitPerMinute
}

// rpcLocalLoadTestRateLimitBypass reports whether a loopback load-test request
// carries the dedicated load-test secret.
func rpcLocalLoadTestRateLimitBypass(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost || len(rpcLocalLoadTestToken) < 32 {
		return false
	}
	switch r.URL.Path {
	case "/submitTx", "/v1/submitTx", "/faucet":
	default:
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
	want := []byte("Bearer " + rpcLocalLoadTestToken)
	return len(got) == len(want) && subtle.ConstantTimeCompare(got, want) == 1
}

func rpcLoopbackReadRateLimitBypass(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch r.URL.Path {
	case "/status", "/v1/status", "/health", "/metrics":
	default:
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// rpcLimiterBurst implements the rpc limiter burst helper.
func rpcLimiterBurst(limitPerMinute int) int {
	if limitPerMinute <= 0 {
		return 0
	}
	// `burst` stores the value produced by this operation.
	burst := limitPerMinute / 10
	if burst < 1 {
		burst = 1
	}
	if burst > 100 {
		burst = 100
	}
	return burst
}

// rpcAllowRequest implements the rpc allow request helper.
func rpcAllowRequest(key string, limitPerMinute int) bool {
	if limitPerMinute <= 0 {
		return true
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	rpcLimiterMu.Lock()
	defer rpcLimiterMu.Unlock()

	// `entry` and `ok` store whether the related condition is satisfied.
	entry, ok := rpcLimiters[key]
	if !ok || entry.limiter == nil {
		entry.limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(limitPerMinute)), rpcLimiterBurst(limitPerMinute))
	}
	entry.lastSeen = now
	rpcLimiters[key] = entry

	if atomic.AddUint64(&rpcLimiterCleanEvery, 1)%512 == 0 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-10 * time.Minute)
		// `k` and `v` track the current values while iterating.
		for k, v := range rpcLimiters {
			if v.lastSeen.Before(cutoff) {
				delete(rpcLimiters, k)
			}
		}
	}

	return entry.limiter.Allow()
}

// rpcAcquireSlot implements the rpc acquire slot helper.
func rpcAcquireSlot(maxConcurrent int) bool {
	if maxConcurrent <= 0 {
		return true
	}
	for {
		// `current` stores the value produced by this operation.
		current := atomic.LoadInt64(&rpcInFlight)
		if current >= int64(maxConcurrent) {
			return false
		}
		if atomic.CompareAndSwapInt64(&rpcInFlight, current, current+1) {
			return true
		}
	}
}

// rpcReleaseSlot implements the rpc release slot helper.
func rpcReleaseSlot(maxConcurrent int) {
	if maxConcurrent <= 0 {
		return
	}
	atomic.AddInt64(&rpcInFlight, -1)
}

// withRPCHardening implements the with rpc hardening helper.
func withRPCHardening(node *Node, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

		// `maxBody` stores the value produced by this operation.
		maxBody := ConfigRPCMaxRequestBodyBytes
		if maxBody > 0 && r != nil {
			if r.ContentLength > maxBody {
				node.observeRPCBodyRejected()
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			}
		}

		if !rpcAcquireSlot(ConfigRPCMaxConcurrentRequests) {
			node.observeRPCConcurrentRejected()
			http.Error(w, "too many concurrent requests", http.StatusTooManyRequests)
			return
		}
		defer rpcReleaseSlot(ConfigRPCMaxConcurrentRequests)

		// `class` stores the value produced by this operation.
		class := rpcRequestClass(r)
		if !rpcLocalLoadTestRateLimitBypass(r) && !rpcLoopbackReadRateLimitBypass(r) && !rpcAllowRequest(rpcClientKey(r, class), rpcLimiterConfig(class)) {
			node.observeRPCRateLimited()
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		node.observeRPCRequestStart()
		if r != nil && (r.URL.Path == "/wallet/events" || r.URL.Path == "/v1/wallet/events") {
			defer node.observeRPCRequestFinish(http.StatusSwitchingProtocols)
			next.ServeHTTP(w, r)
			return
		}
		// `recorder` stores the value produced by this operation.
		recorder := &rpcStatusRecorder{ResponseWriter: w}
		defer func() {
			node.observeRPCRequestFinish(recorder.status)
		}()
		next.ServeHTTP(recorder, r)
	})
}

// newRPCServer implements the new rpc server helper.
func newRPCServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: rpcTimeout(ConfigRPCReadHeaderTimeoutSeconds),
		ReadTimeout:       rpcTimeout(ConfigRPCReadTimeoutSeconds),
		WriteTimeout:      rpcTimeout(ConfigRPCWriteTimeoutSeconds),
		IdleTimeout:       rpcTimeout(ConfigRPCIdleTimeoutSeconds),
	}
}
