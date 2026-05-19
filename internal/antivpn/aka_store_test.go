package antivpn

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAkaStoreRecordsPreviousNameForSameIP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aka.db")
	store := newAkaStore(Config{
		AkaEnabled:    true,
		AkaPath:       path,
		AkaMaxEntries: 20000,
		AkaTTL:        365 * 24 * time.Hour,
	}, nil)

	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	if aka := store.Record("198.51.100.25", "Padawan", "Sweden", now); aka != "-" {
		t.Fatalf("expected first AKA lookup to be empty, got %q", aka)
	}
	if aka := store.Record("198.51.100.25", "Aki", "Sweden", now.Add(time.Minute)); aka != "Padawan" {
		t.Fatalf("expected previous name Padawan, got %q", aka)
	}
	if aka := store.Record("198.51.100.25", "ASF", "Unknown", now.Add(2*time.Minute)); aka != "Aki" {
		t.Fatalf("expected previous name Aki, got %q", aka)
	}
}

func TestAkaStorePersistsRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aka.db")
	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)

	store := newAkaStore(Config{
		AkaEnabled:    true,
		AkaPath:       path,
		AkaMaxEntries: 20000,
		AkaTTL:        365 * 24 * time.Hour,
	}, nil)
	store.Record("198.51.100.25", "Padawan", "Sweden", now)

	reloaded := newAkaStore(Config{
		AkaEnabled:    true,
		AkaPath:       path,
		AkaMaxEntries: 20000,
		AkaTTL:        365 * 24 * time.Hour,
	}, nil)
	if aka := reloaded.Record("198.51.100.25", "Aki", "Sweden", now.Add(time.Minute)); aka != "Padawan" {
		t.Fatalf("expected persisted previous name Padawan, got %q", aka)
	}
}

func TestAkaStorePrunesMaxEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aka.db")
	store := newAkaStore(Config{
		AkaEnabled:    true,
		AkaPath:       path,
		AkaMaxEntries: 2,
		AkaTTL:        365 * 24 * time.Hour,
	}, nil)

	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	store.Record("198.51.100.1", "One", "Sweden", now)
	store.Record("198.51.100.2", "Two", "Sweden", now.Add(time.Minute))
	store.Record("198.51.100.3", "Three", "Sweden", now.Add(2*time.Minute))

	if len(store.records) != 2 {
		t.Fatalf("expected 2 AKA records after prune, got %d", len(store.records))
	}
	if _, exists := store.records["198.51.100.1"]; exists {
		t.Fatal("expected oldest AKA record to be pruned")
	}
}
