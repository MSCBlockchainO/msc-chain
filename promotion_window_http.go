package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// promotionWindowResponse implements the promotion window response helper.
func (s *Server) promotionWindowResponse(r *http.Request) (map[string]any, int, string) {
	if s == nil || s.Node == nil {
		return nil, http.StatusServiceUnavailable, "node unavailable"
	}
	// `height` stores the value produced by this operation.
	height := uint64(1)
	if s.Node.Blockchain != nil {
		height = s.Node.Blockchain.Height() + 1
	}
	// `window` stores the value produced by this operation.
	window := validatorHybridPromotionWindowBucket(height)
	// `raw` stores the value produced by this operation.
	if raw := strings.TrimSpace(r.URL.Query().Get("window")); raw != "" {
		// `parsed` and `err` store the error produced by this operation.
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, http.StatusBadRequest, "invalid window"
		}
		window = parsed
	}
	// `record`, `replacements`, `hash`, and `err` store the error produced by this operation.
	record, replacements, hash, err := s.Node.loadPromotionWindowState(window)
	// `resp` stores the response produced by this operation.
	resp := map[string]any{
		"available":         err == nil && record != nil,
		"window":            window,
		"current_window":    validatorHybridPromotionWindowBucket(height),
		"activation_height": PromotionWindowRecordV1Height,
		"enabled":           promotionWindowRecordV1EnabledAt(height),
	}
	if err == nil && record != nil {
		resp["record"] = normalizePromotionWindowRecord(*record)
		resp["replacements"] = replacements
		resp["record_hash"] = PromotionWindowRecordHash(*record)
		resp["promotion_window_hash"] = strings.TrimSpace(hash)
		resp["effective_performance_validators"] = promotionWindowEffectivePerformanceIDs(record, replacements)
		resp["replacement_count"] = len(replacements)
	} else {
		resp["reason"] = "not_found"
	}
	return resp, http.StatusOK, ""
}

// handleValidatorsPromotionWindow handles validators promotion window.
func (s *Server) handleValidatorsPromotionWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `resp`, `status`, and `msg` store the response produced by this operation.
	resp, status, msg := s.promotionWindowResponse(r)
	if status != http.StatusOK {
		http.Error(w, msg, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleV1ValidatorsPromotionWindow handles v1 validators promotion window.
func (s *Server) handleV1ValidatorsPromotionWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	// `resp`, `status`, and `msg` store the response produced by this operation.
	resp, status, msg := s.promotionWindowResponse(r)
	if status != http.StatusOK {
		writeV1Error(w, status, "", msg)
		return
	}
	writeV1Data(w, http.StatusOK, resp)
}
