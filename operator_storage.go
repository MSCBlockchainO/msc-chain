package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// operatorStorageCommand implements the operator storage command helper.
func operatorStorageCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: storage metrics|compact --path <pebble-db-path>")
	}
	// `subcommand` stores the value produced by this operation.
	subcommand := strings.ToLower(strings.TrimSpace(args[0]))
	switch subcommand {
	case "metrics":
		return operatorStorageMetrics(args[1:])
	case "compact":
		return operatorStorageCompact(args[1:])
	default:
		return fmt.Errorf("unknown storage command %q", subcommand)
	}
}

// operatorStorageMetrics implements the operator storage metrics helper.
func operatorStorageMetrics(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("storage metrics", flag.ContinueOnError)
	// `path` stores the value produced by this operation.
	path := fs.String("path", "", "Pebble database directory")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `cleaned` and `err` store the error produced by this operation.
	cleaned, err := operatorStoragePath(*path)
	if err != nil {
		return err
	}
	// `db` and `err` store the error produced by this operation.
	db, err := openPebbleDB(cleaned)
	if err != nil {
		return fmt.Errorf("open database (stop the node if it is using this path): %w", err)
	}
	defer db.Close()
	// `metrics` and `err` store the error produced by this operation.
	metrics, err := db.MetricsSummary()
	if err != nil {
		return err
	}
	operatorPrintJSON(map[string]interface{}{
		"action":  "storage_metrics",
		"path":    cleaned,
		"metrics": metrics,
	})
	return nil
}

// operatorStorageCompact implements the operator storage compact helper.
func operatorStorageCompact(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("storage compact", flag.ContinueOnError)
	// `path` stores the value produced by this operation.
	path := fs.String("path", "", "Pebble database directory")
	// `parallel` stores the value produced by this operation.
	parallel := fs.Bool("parallel", false, "allow parallel manual compaction")
	// `confirmOffline` stores the value produced by this operation.
	confirmOffline := fs.Bool("confirm-offline", false, "confirm that the node using this database is stopped")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*confirmOffline {
		return fmt.Errorf("storage compact requires --confirm-offline; stop the node before compacting")
	}
	// `cleaned` and `err` store the error produced by this operation.
	cleaned, err := operatorStoragePath(*path)
	if err != nil {
		return err
	}
	// `db` and `err` store the error produced by this operation.
	db, err := openPebbleDB(cleaned)
	if err != nil {
		return fmt.Errorf("open database (node must be stopped): %w", err)
	}
	defer db.Close()
	// `before` and `err` store the error produced by this operation.
	before, err := db.MetricsSummary()
	if err != nil {
		return err
	}
	// `started` stores the value produced by this operation.
	started := time.Now()
	// `err` stores the error produced by this operation.
	if err := db.CompactAll(*parallel); err != nil {
		return fmt.Errorf("compact database: %w", err)
	}
	// `after` and `err` store the error produced by this operation.
	after, err := db.MetricsSummary()
	if err != nil {
		return err
	}
	operatorPrintJSON(map[string]interface{}{
		"action":           "storage_compact",
		"path":             cleaned,
		"parallel":         *parallel,
		"duration_seconds": time.Since(started).Seconds(),
		"before":           before,
		"after":            after,
	})
	return nil
}

// operatorStoragePath implements the operator storage path helper.
func operatorStoragePath(path string) (string, error) {
	// `cleaned` stores the value produced by this operation.
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return "", fmt.Errorf("--path is required")
	}
	// `absolute` and `err` store the error produced by this operation.
	absolute, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}
