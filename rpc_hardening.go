package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

type rpcLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	rpcLimiterMu         sync.Mutex
	rpcLimiters          = make(map[string]rpcLimiterEntry)
	rpcLimiterCleanEvery uint64
	rpcInFlight          int64
)

type rpcStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *rpcStatusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *rpcStatusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func rpcTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func rpcRequestClass(r *http.Request) string {
	if r == nil {
		return "read"
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return "read"
	}
	return "write"
}

func rpcClientKey(r *http.Request, class string) string {
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

func rpcLimiterConfig(class string) int {
	if class == "write" {
		return ConfigRPCWriteRateLimitPerMinute
	}
	return ConfigRPCReadRateLimitPerMinute
}

func rpcLimiterBurst(limitPerMinute int) int {
	if limitPerMinute <= 0 {
		return 0
	}
	burst := limitPerMinute / 10
	if burst < 1 {
		burst = 1
	}
	if burst > 100 {
		burst = 100
	}
	return burst
}

func rpcAllowRequest(key string, limitPerMinute int) bool {
	if limitPerMinute <= 0 {
		return true
	}
	now := time.Now()
	rpcLimiterMu.Lock()
	defer rpcLimiterMu.Unlock()

	entry, ok := rpcLimiters[key]
	if !ok || entry.limiter == nil {
		entry.limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(limitPerMinute)), rpcLimiterBurst(limitPerMinute))
	}
	entry.lastSeen = now
	rpcLimiters[key] = entry

	if atomic.AddUint64(&rpcLimiterCleanEvery, 1)%512 == 0 {
		cutoff := now.Add(-10 * time.Minute)
		for k, v := range rpcLimiters {
			if v.lastSeen.Before(cutoff) {
				delete(rpcLimiters, k)
			}
		}
	}

	return entry.limiter.Allow()
}

func rpcAcquireSlot(maxConcurrent int) bool {
	if maxConcurrent <= 0 {
		return true
	}
	for {
		current := atomic.LoadInt64(&rpcInFlight)
		if current >= int64(maxConcurrent) {
			return false
		}
		if atomic.CompareAndSwapInt64(&rpcInFlight, current, current+1) {
			return true
		}
	}
}

func rpcReleaseSlot(maxConcurrent int) {
	if maxConcurrent <= 0 {
		return
	}
	atomic.AddInt64(&rpcInFlight, -1)
}

func withRPCHardening(node *Node, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

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

		class := rpcRequestClass(r)
		if !rpcAllowRequest(rpcClientKey(r, class), rpcLimiterConfig(class)) {
			node.observeRPCRateLimited()
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		node.observeRPCRequestStart()
		recorder := &rpcStatusRecorder{ResponseWriter: w}
		defer func() {
			node.observeRPCRequestFinish(recorder.status)
		}()
		next.ServeHTTP(recorder, r)
	})
}

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
