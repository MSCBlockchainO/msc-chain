package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestDBCompactAllPreservesValues(t *testing.T) {
	db, err := openPebbleDB(filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	for round := 0; round < 40; round++ {
		if err := db.Update(func(txn *Txn) error {
			for key := 0; key < 20; key++ {
				name := []byte(fmt.Sprintf("meta:%03d", key))
				if err := txn.Set(name, []byte(fmt.Sprintf("round-%03d", round))); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("write round %d: %v", round, err)
		}
	}

	if _, err := db.MetricsSummary(); err != nil {
		t.Fatalf("metrics before compact: %v", err)
	}
	if err := db.CompactAll(false); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if _, err := db.MetricsSummary(); err != nil {
		t.Fatalf("metrics after compact: %v", err)
	}
	if err := db.View(func(txn *Txn) error {
		item, err := txn.Get([]byte("meta:007"))
		if err != nil {
			return err
		}
		return item.Value(func(value []byte) error {
			if got, want := string(value), "round-039"; got != want {
				return fmt.Errorf("value = %q, want %q", got, want)
			}
			return nil
		})
	}); err != nil {
		t.Fatalf("verify compacted value: %v", err)
	}
}

func TestOperatorStorageCompactRequiresOfflineConfirmation(t *testing.T) {
	err := operatorStorageCompact([]string{"--path", filepath.Join(t.TempDir(), "meta.db")})
	if err == nil {
		t.Fatalf("expected offline confirmation requirement")
	}
}
