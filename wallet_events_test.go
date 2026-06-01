package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWalletEventsWebSocketHello(t *testing.T) {
	oldRequireRead := ConfigRPCRequireAuthForReadEndpoints
	oldAPIToken := apiToken
	defer func() {
		ConfigRPCRequireAuthForReadEndpoints = oldRequireRead
		apiToken = oldAPIToken
	}()
	ConfigRPCRequireAuthForReadEndpoints = false
	apiToken = ""

	node := &Node{
		ID:   "F",
		Role: "full",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 1, BlockHash: "h1"}},
		},
		lastCommitHeight: 1,
		lastCommitAt:     time.Now(),
	}
	server := NewServer(node)
	mux := http.NewServeMux()
	mux.HandleFunc("/wallet/events", server.handleWalletEvents)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/wallet/events"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial wallet events: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read hello event: %v", err)
	}
	var event walletEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatalf("decode hello event: %v body=%s", err, string(raw))
	}
	if event.Type != "hello" {
		t.Fatalf("unexpected event type: got=%s want=hello payload=%+v", event.Type, event)
	}
	if event.Height != 1 || event.FinalizedHeight != 1 || event.Hash != "h1" {
		t.Fatalf("unexpected hello chain fields: %+v", event)
	}
}

func TestWalletEventsThroughRPCHardening(t *testing.T) {
	oldRequireRead := ConfigRPCRequireAuthForReadEndpoints
	oldAPIToken := apiToken
	defer func() {
		ConfigRPCRequireAuthForReadEndpoints = oldRequireRead
		apiToken = oldAPIToken
	}()
	ConfigRPCRequireAuthForReadEndpoints = false
	apiToken = ""

	node := &Node{
		ID:   "F",
		Role: "full",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 1, BlockHash: "h1"}},
		},
		lastCommitHeight: 1,
		lastCommitAt:     time.Now(),
	}
	server := NewServer(node)
	mux := http.NewServeMux()
	mux.HandleFunc("/wallet/events", server.handleWalletEvents)
	ts := httptest.NewServer(withRPCHardening(node, mux))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/wallet/events"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial wallet events through hardening: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var event walletEvent
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read hardened hello event: %v", err)
	}
	if event.Type != "hello" || event.Height != 1 {
		t.Fatalf("unexpected hardened hello event: %+v", event)
	}
}

func TestWalletEventsRejectsNonGet(t *testing.T) {
	server := NewServer(&Node{})
	req := httptest.NewRequest(http.MethodPost, "/wallet/events", nil)
	rr := httptest.NewRecorder()
	server.handleWalletEvents(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}
