package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func operatorStorageCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: storage metrics|compact --path <pebble-db-path>")
	}
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

func operatorStorageMetrics(args []string) error {
	fs := flag.NewFlagSet("storage metrics", flag.ContinueOnError)
	path := fs.String("path", "", "Pebble database directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cleaned, err := operatorStoragePath(*path)
	if err != nil {
		return err
	}
	db, err := openPebbleDB(cleaned)
	if err != nil {
		return fmt.Errorf("open database (stop the node if it is using this path): %w", err)
	}
	defer db.Close()
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

func operatorStorageCompact(args []string) error {
	fs := flag.NewFlagSet("storage compact", flag.ContinueOnError)
	path := fs.String("path", "", "Pebble database directory")
	parallel := fs.Bool("parallel", false, "allow parallel manual compaction")
	confirmOffline := fs.Bool("confirm-offline", false, "confirm that the node using this database is stopped")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*confirmOffline {
		return fmt.Errorf("storage compact requires --confirm-offline; stop the node before compacting")
	}
	cleaned, err := operatorStoragePath(*path)
	if err != nil {
		return err
	}
	db, err := openPebbleDB(cleaned)
	if err != nil {
		return fmt.Errorf("open database (node must be stopped): %w", err)
	}
	defer db.Close()
	before, err := db.MetricsSummary()
	if err != nil {
		return err
	}
	started := time.Now()
	if err := db.CompactAll(*parallel); err != nil {
		return fmt.Errorf("compact database: %w", err)
	}
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

func operatorStoragePath(path string) (string, error) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return "", fmt.Errorf("--path is required")
	}
	absolute, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}
