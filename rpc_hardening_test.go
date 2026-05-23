package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rpcHardeningConfigSnapshot struct {
	maxBody       int64
	readRPM       int
	writeRPM      int
	maxConcurrent int
}

func snapshotRPCHardeningConfig() rpcHardeningConfigSnapshot {
	return rpcHardeningConfigSnapshot{
		maxBody:       ConfigRPCMaxRequestBodyBytes,
		readRPM:       ConfigRPCReadRateLimitPerMinute,
		writeRPM:      ConfigRPCWriteRateLimitPerMinute,
		maxConcurrent: ConfigRPCMaxConcurrentRequests,
	}
}

func (s rpcHardeningConfigSnapshot) restore() {
	ConfigRPCMaxRequestBodyBytes = s.maxBody
	ConfigRPCReadRateLimitPerMinute = s.readRPM
	ConfigRPCWriteRateLimitPerMinute = s.writeRPM
	ConfigRPCMaxConcurrentRequests = s.maxConcurrent
}

func resetRPCHardeningLimitersForTest() {
	rpcLimiterMu.Lock()
	rpcLimiters = make(map[string]rpcLimiterEntry)
	rpcLimiterMu.Unlock()
}

func TestRPCHardeningAddsSecurityHeaders(t *testing.T) {
	cfg := snapshotRPCHardeningConfig()
	defer cfg.restore()
	resetRPCHardeningLimitersForTest()
	ConfigRPCReadRateLimitPerMinute = 0
	ConfigRPCWriteRateLimitPerMinute = 0

	node := &Node{}
	handler := withRPCHardening(node, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: got=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("missing nosniff header: %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("missing frame deny header: %q", got)
	}
	obs := node.observabilityStatsSnapshot()
	if obs.RPCRequestsTotal != 1 || obs.RPCInflight != 0 {
		t.Fatalf("unexpected rpc counters: %+v", obs)
	}
}

func TestRPCHardeningRejectsOversizedBodyBeforeHandler(t *testing.T) {
	cfg := snapshotRPCHardeningConfig()
	defer cfg.restore()
	resetRPCHardeningLimitersForTest()
	ConfigRPCMaxRequestBodyBytes = 8
	ConfigRPCWriteRateLimitPerMinute = 0
	ConfigRPCMaxConcurrentRequests = 0

	node := &Node{}
	called := false
	handler := withRPCHardening(node, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/submitTx", strings.NewReader("0123456789"))
	req.RemoteAddr = "127.0.0.1:12346"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got=%d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatalf("handler should not be called for oversized body")
	}
	obs := node.observabilityStatsSnapshot()
	if obs.RPCBodyRejectedTotal != 1 || obs.RPCRequestsTotal != 0 {
		t.Fatalf("unexpected rpc counters: %+v", obs)
	}
}

func TestRPCHardeningRateLimitsPerClientClass(t *testing.T) {
	cfg := snapshotRPCHardeningConfig()
	defer cfg.restore()
	resetRPCHardeningLimitersForTest()
	ConfigRPCReadRateLimitPerMinute = 1
	ConfigRPCWriteRateLimitPerMinute = 0
	ConfigRPCMaxConcurrentRequests = 0

	node := &Node{}
	handler := withRPCHardening(node, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		req.RemoteAddr = "127.0.0.9:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusOK {
			t.Fatalf("first request should pass, got=%d", rec.Code)
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second request should be rate limited, got=%d", rec.Code)
		}
	}
	obs := node.observabilityStatsSnapshot()
	if obs.RPCRateLimitedTotal != 1 || obs.RPCRequestsTotal != 1 {
		t.Fatalf("unexpected rpc counters: %+v", obs)
	}
}
