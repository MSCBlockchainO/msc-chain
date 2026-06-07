package main

import (
	"os"
	"strings"
	"testing"
)

func longTestsEnabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("MSC_RUN_LONG_TESTS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func skipLongTestUnlessEnabled(t *testing.T, reason string) {
	t.Helper()
	if longTestsEnabled() {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "long-running scale test"
	}
	t.Skipf("%s; set MSC_RUN_LONG_TESTS=1 to run", reason)
}
