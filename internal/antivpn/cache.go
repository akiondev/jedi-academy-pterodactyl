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
	path          string
	flushInterval time.Duration
	logger        *slog.Logger

	mu      sync.RWMutex
	entries map[string]Decision
	dirty   bool

	closeOnce sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

type persistedCache struct {
	Entries map[string]Decision `json:"entries"`
}

func NewCache(path string, flushInterval time.Duration, logger *slog.Logger) (*Cache, error) {
	cache := &Cache{
		path:          path,
		flushInterval: flushInterval,
		logger:        logger,
		entries:       make(map[string]Decision),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
	if err := cache.load(); err != nil {
		if logger != nil {
			logger.Warn("anti-vpn cache load failed, continuing with empty cache", "path", path, "error", err)
		}
	}

	if cache.path == "" {
		close(cache.doneCh)
		return cache, nil
	}

	go cache.flushLoop()
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
		c.dirty = true
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
	c.dirty = true
}

func (c *Cache) Close() error {
	var err error

	c.closeOnce.Do(func() {
		if c.path == "" {
			return
		}

		close(c.stopCh)
		<-c.doneCh
		err = c.flushDirty()
	})

	return err
}

func (c *Cache) flushLoop() {
	defer close(c.doneCh)

	ticker := time.NewTicker(c.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.flushDirty(); err != nil && c.logger != nil {
				c.logger.Warn("anti-vpn cache save failed", "path", c.path, "error", err)
			}
		case <-c.stopCh:
			return
		}
	}
}

func (c *Cache) flushDirty() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.dirty {
		return nil
	}
	if err := c.saveLocked(); err != nil {
		return err
	}
	c.dirty = false
	return nil
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
