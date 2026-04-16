package antivpn

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Cache struct {
	path    string
	logger  *slog.Logger
	mu      sync.RWMutex
	entries map[string]Decision
}

type persistedCache struct {
	Entries map[string]Decision `json:"entries"`
}

func NewCache(path string, logger *slog.Logger) (*Cache, error) {
	cache := &Cache{
		path:    path,
		logger:  logger,
		entries: make(map[string]Decision),
	}
	if err := cache.load(); err != nil {
		if logger != nil {
			logger.Warn("anti-vpn cache load failed, continuing with empty cache", "path", path, "error", err)
		}
	}
	return cache, nil
}

func (c *Cache) Get(ip string) (Decision, bool) {
	now := time.Now().UTC()

	c.mu.RLock()
	decision, ok := c.entries[ip]
	c.mu.RUnlock()
	if !ok {
		return Decision{}, false
	}
	if !decision.ExpiresAt.IsZero() && now.After(decision.ExpiresAt) {
		c.mu.Lock()
		delete(c.entries, ip)
		c.mu.Unlock()
		return Decision{}, false
	}

	decision.FromCache = true
	return decision, true
}

func (c *Cache) Set(decision Decision) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[decision.IP] = decision
	if err := c.saveLocked(); err != nil && c.logger != nil {
		c.logger.Warn("anti-vpn cache save failed", "path", c.path, "error", err)
	}
}

func (c *Cache) load() error {
	if c.path == "" {
		return nil
	}

	data, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var payload persistedCache
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	now := time.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()

	for ip, decision := range payload.Entries {
		if decision.ExpiresAt.IsZero() || now.Before(decision.ExpiresAt) {
			c.entries[ip] = decision
		}
	}
	return nil
}

func (c *Cache) saveLocked() error {
	if c.path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}

	tmpPath := c.path + ".tmp"
	payload := persistedCache{
		Entries: c.entries,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, c.path)
}
