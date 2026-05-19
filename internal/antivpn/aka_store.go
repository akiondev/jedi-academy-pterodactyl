package antivpn

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type akaRecord struct {
	LatestName   string    `json:"latest_name"`
	PreviousName string    `json:"previous_name,omitempty"`
	Country      string    `json:"country,omitempty"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	SeenCount    int       `json:"seen_count"`
}

type akaStoreFile struct {
	Version int                  `json:"version"`
	Records map[string]akaRecord `json:"records"`
}

type akaStore struct {
	path       string
	maxEntries int
	ttl        time.Duration
	logger     *slog.Logger

	mu      sync.Mutex
	records map[string]akaRecord
}

func newAkaStore(cfg Config, logger *slog.Logger) *akaStore {
	if !cfg.AkaEnabled || strings.TrimSpace(cfg.AkaPath) == "" {
		return nil
	}

	store := &akaStore{
		path:       cfg.AkaPath,
		maxEntries: cfg.AkaMaxEntries,
		ttl:        cfg.AkaTTL,
		logger:     logger,
		records:    make(map[string]akaRecord),
	}
	store.load()
	return store
}

func (s *akaStore) Record(ip, playerName, country string, now time.Time) string {
	if s == nil {
		return "-"
	}

	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "-"
	}

	name := sanitizePlayerName(playerName)
	country = sanitizeCountryForConsoleCommand(country)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(now)

	record, exists := s.records[ip]
	aka := "-"
	if exists {
		if !sameAkaName(record.LatestName, name) {
			if strings.TrimSpace(record.LatestName) != "" {
				aka = record.LatestName
				record.PreviousName = record.LatestName
			}
			record.LatestName = name
		} else if strings.TrimSpace(record.PreviousName) != "" && !sameAkaName(record.PreviousName, name) {
			aka = record.PreviousName
		}
	} else {
		record.FirstSeen = now
		record.LatestName = name
	}

	record.Country = country
	record.LastSeen = now
	record.SeenCount++
	if record.FirstSeen.IsZero() {
		record.FirstSeen = now
	}
	s.records[ip] = record

	s.pruneLocked(now)
	if err := s.saveLocked(); err != nil && s.logger != nil {
		s.logger.Warn("anti-vpn AKA store save failed", "path", s.path, "error", err)
	}

	return aka
}

func (s *akaStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && s.logger != nil {
			s.logger.Warn("anti-vpn AKA store read failed", "path", s.path, "error", err)
		}
		return
	}

	var wrapped akaStoreFile
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Records != nil {
		s.records = wrapped.Records
		return
	}

	var legacy map[string]akaRecord
	if err := json.Unmarshal(data, &legacy); err == nil && legacy != nil {
		s.records = legacy
		return
	}

	if s.logger != nil {
		s.logger.Warn("anti-vpn AKA store is not valid JSON; starting with empty store", "path", s.path)
	}
}

func (s *akaStore) saveLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(akaStoreFile{Version: 1, Records: s.records}, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *akaStore) pruneLocked(now time.Time) {
	if s.ttl > 0 {
		cutoff := now.Add(-s.ttl)
		for ip, record := range s.records {
			if !record.LastSeen.IsZero() && record.LastSeen.Before(cutoff) {
				delete(s.records, ip)
			}
		}
	}

	if s.maxEntries <= 0 || len(s.records) <= s.maxEntries {
		return
	}

	type entry struct {
		ip       string
		lastSeen time.Time
	}
	entries := make([]entry, 0, len(s.records))
	for ip, record := range s.records {
		entries = append(entries, entry{ip: ip, lastSeen: record.LastSeen})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastSeen.Before(entries[j].lastSeen)
	})

	for i := 0; i < len(entries)-s.maxEntries; i++ {
		delete(s.records, entries[i].ip)
	}
}

func sameAkaName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
