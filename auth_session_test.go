package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestHandleAuthChallengeReplacesMismatchedSession(t *testing.T) {
	authMu.Lock()
	oldSessions := authSessions
	oldTokens := authTokens
	oldReady := authReady
	oldNodeID := authNodeID
	oldWalletAddr := authWalletAddr
	oldWalletPub := authWalletPub
	authSessions = make(map[string]*AuthSession)
	authTokens = make(map[string]*AuthSession)
	authReady = false
	authNodeID = ""
	authWalletAddr = ""
	authWalletPub = ""
	authMu.Unlock()
	t.Cleanup(func() {
		authMu.Lock()
		authSessions = oldSessions
		authTokens = oldTokens
		authReady = oldReady
		authNodeID = oldNodeID
		authWalletAddr = oldWalletAddr
		authWalletPub = oldWalletPub
		authMu.Unlock()
	})

	stale := initAuthSession("A")
	if stale == nil || stale.SessionID == "" {
		t.Fatalf("expected stale auth session")
	}

	server := &Server{Node: &Node{ID: "B"}}
	req := httptest.NewRequest(http.MethodGet, "/auth/challenge?session="+stale.SessionID+"&node_id=B", nil)
	rec := httptest.NewRecorder()

	server.handleAuthChallenge(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		SessionID string `json:"session_id"`
		NodeID    string `json:"node_id"`
		Message   string `json:"message"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.NodeID != "B" {
		t.Fatalf("expected replacement challenge for node B, got %q", payload.NodeID)
	}
	if payload.SessionID == "" || payload.SessionID == stale.SessionID {
		t.Fatalf("expected new session id, got %q", payload.SessionID)
	}
	if payload.Message == "" || payload.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("expected valid challenge payload, got %+v", payload)
	}

	authMu.Lock()
	_, staleStillPresent := authSessions[stale.SessionID]
	replacement := authSessions[payload.SessionID]
	authMu.Unlock()
	if staleStillPresent {
		t.Fatalf("expected stale mismatched session to be discarded")
	}
	if replacement == nil || replacement.NodeID != "B" {
		t.Fatalf("expected replacement session stored for node B")
	}
}

func TestBuildAuthURLPreservesOriginAndRPCTarget(t *testing.T) {
	raw := buildAuthURL("127.0.0.1:26663", "Talha", "session-123")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	if parsed.Scheme != "http" || parsed.Host != "127.0.0.1:26663" {
		t.Fatalf("unexpected auth url origin: %s", raw)
	}
	q := parsed.Query()
	if q.Get("node") != "TALHA" {
		t.Fatalf("expected normalized node id, got %q", q.Get("node"))
	}
	if q.Get("session") != "session-123" {
		t.Fatalf("expected session query to round-trip")
	}
	if q.Get("keep_origin") != "1" {
		t.Fatalf("expected auth url to preserve original origin")
	}
	if q.Get("rpc") != "http://127.0.0.1:26663" {
		t.Fatalf("expected auth rpc query, got %q", q.Get("rpc"))
	}
}
