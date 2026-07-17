package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type MisbehaviorSummaryItem struct {
	Validator     string `json:"validator"`
	Count         int    `json:"count"`
	LastReason    string `json:"last_reason"`
	LastHeight    uint64 `json:"last_height"`
	LastBlockHash string `json:"last_block_hash,omitempty"`
	LastTimestamp int64  `json:"last_timestamp"`
}

func (n *Node) misbehaviorSummary(limit int) ([]MisbehaviorSummaryItem, int, int) {
	if n == nil {
		return nil, 0, 0
	}
	if limit <= 0 {
		limit = 100
	}

	n.misbehaviorMu.Lock()
	items := make([]MisbehaviorSummaryItem, 0, len(n.MisbehaviorLog))
	totalEvents := 0
	for validator, entries := range n.MisbehaviorLog {
		if len(entries) == 0 {
			continue
		}
		totalEvents += len(entries)
		last := entries[len(entries)-1]
		items = append(items, MisbehaviorSummaryItem{
			Validator:     validator,
			Count:         len(entries),
			LastReason:    strings.TrimSpace(last.Reason),
			LastHeight:    last.Height,
			LastBlockHash: last.BlockHash,
			LastTimestamp: last.Timestamp,
		})
	}
	n.misbehaviorMu.Unlock()

	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Validator < items[j].Validator
		}
		return items[i].Count > items[j].Count
	})
	totalValidators := len(items)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, totalValidators, totalEvents
}

func (s *Server) handleMisbehavior(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.Node == nil {
		http.Error(w, "node unavailable", http.StatusServiceUnavailable)
		return
	}

	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	items, validatorsTotal, eventsTotal := s.Node.misbehaviorSummary(limit)
	stats := s.Node.MapStats()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"validators_total":      validatorsTotal,
		"events_total":          eventsTotal,
		"exec_mismatch_tracked": stats.ExecMismatchTracked,
		"items":                 items,
	})
}

func (s *Server) handleV1Misbehavior(w http.ResponseWriter, r *http.Request) {
	s.handleMisbehavior(w, r)
}
