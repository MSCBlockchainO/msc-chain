package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type MisbehaviorSummaryItem struct {
	// `Validator` stores whether the related condition is satisfied.
	Validator     string `json:"validator"`
	// `Count` stores the measured quantity used by this operation.
	Count         int    `json:"count"`
	// `LastReason` stores the value associated with this record.
	LastReason    string `json:"last_reason"`
	// `LastHeight` stores the value associated with this record.
	LastHeight    uint64 `json:"last_height"`
	// `LastBlockHash` stores the digest used to identify or verify the related data.
	LastBlockHash string `json:"last_block_hash,omitempty"`
	// `LastTimestamp` stores the value associated with this record.
	LastTimestamp int64  `json:"last_timestamp"`
}

// misbehaviorSummary implements the misbehavior summary helper.
func (n *Node) misbehaviorSummary(limit int) ([]MisbehaviorSummaryItem, int, int) {
	if n == nil {
		return nil, 0, 0
	}
	if limit <= 0 {
		limit = 100
	}

	n.misbehaviorMu.Lock()
	// `items` stores the current position in the related collection.
	items := make([]MisbehaviorSummaryItem, 0, len(n.MisbehaviorLog))
	// `totalEvents` stores the measured quantity used by this operation.
	totalEvents := 0
	// `validator` and `entries` track whether the related condition is satisfied.
	for validator, entries := range n.MisbehaviorLog {
		if len(entries) == 0 {
			continue
		}
		totalEvents += len(entries)
		// `last` stores the value produced by this operation.
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
	// `totalValidators` stores the measured quantity used by this operation.
	totalValidators := len(items)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, totalValidators, totalEvents
}

// handleMisbehavior handles misbehavior.
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

	// `limit` stores the value produced by this operation.
	limit := 100
	// `raw` stores the value produced by this operation.
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		// `parsed` and `err` store the error produced by this operation.
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	// `items`, `validatorsTotal`, and `eventsTotal` store whether the related condition is satisfied.
	items, validatorsTotal, eventsTotal := s.Node.misbehaviorSummary(limit)
	// `stats` stores the value produced by this operation.
	stats := s.Node.MapStats()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"validators_total":      validatorsTotal,
		"events_total":          eventsTotal,
		"exec_mismatch_tracked": stats.ExecMismatchTracked,
		"items":                 items,
	})
}

// handleV1Misbehavior handles v1 misbehavior.
func (s *Server) handleV1Misbehavior(w http.ResponseWriter, r *http.Request) {
	s.handleMisbehavior(w, r)
}
