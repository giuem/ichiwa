package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Limits of host.storage and host.cache (§8).
const (
	MaxStorageBytes   = 64 << 10
	MaxCacheBytes     = 4 << 20
	MaxCacheItemBytes = 256 << 10
)

// Storage is a plugin's persistent key-value store. Its size is the bytes
// of its keys and values. With a path it is saved there as JSON after
// every change; without one it lives in memory.
type Storage struct {
	mu     sync.Mutex
	path   string
	values map[string]string
	size   int
}

// OpenStorage loads the storage saved at path, or an empty one; "" keeps
// it in memory only.
func OpenStorage(path string) *Storage {
	s := &Storage{path: path, values: map[string]string{}}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return s
	}
	for k, v := range m {
		if s.size+len(k)+len(v) > MaxStorageBytes {
			continue
		}
		s.values[k] = v
		s.size += len(k) + len(v)
	}
	return s
}

func (s *Storage) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	return v, ok
}

// Set stores value at key, or fails when the storage would pass
// MaxStorageBytes.
func (s *Storage) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.values[key]
	size := s.size + len(key) + len(value)
	if had {
		size -= len(key) + len(old)
	}
	if size > MaxStorageBytes {
		return fmt.Errorf("host.storage 超出 %d KiB 的总量", MaxStorageBytes>>10)
	}
	prev := s.size
	s.values[key] = value
	s.size = size
	if err := s.save(); err != nil {
		if had {
			s.values[key] = old
		} else {
			delete(s.values, key)
		}
		s.size = prev
		return err
	}
	return nil
}

func (s *Storage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.values[key]
	if !had {
		return nil
	}
	delete(s.values, key)
	s.size -= len(key) + len(old)
	if err := s.save(); err != nil {
		s.values[key] = old
		s.size += len(key) + len(old)
		return err
	}
	return nil
}

func (s *Storage) save() error {
	if s.path == "" {
		return nil
	}
	data, err := json.Marshal(s.values)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return errors.New("host.storage 保存失败")
	}
	return nil
}

// Cache is a plugin's in-memory cache: MaxCacheBytes in all,
// MaxCacheItemBytes an item, the oldest written dropped first.
type Cache struct {
	mu    sync.Mutex
	items map[string]*cacheItem
	size  int
	seq   uint64
	now   func() time.Time
}

type cacheItem struct {
	value   string
	expires time.Time
	seq     uint64
}

func NewCache() *Cache { return &Cache{items: map[string]*cacheItem{}, now: time.Now} }

func (c *Cache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok {
		return "", false
	}
	if !it.expires.IsZero() && !c.now().Before(it.expires) {
		c.drop(key)
		return "", false
	}
	return it.value, true
}

// Set stores value for ttl (0: until the app quits). An item larger than
// MaxCacheItemBytes is an error; older items make room for a new one.
func (c *Cache) Set(key, value string, ttl time.Duration) error {
	n := len(key) + len(value)
	if n > MaxCacheItemBytes {
		return fmt.Errorf("host.cache 单项不能超过 %d KiB", MaxCacheItemBytes>>10)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drop(key)
	now := c.now()
	for k, it := range c.items {
		if !it.expires.IsZero() && !now.Before(it.expires) {
			c.drop(k)
		}
	}
	for c.size+n > MaxCacheBytes {
		var oldest string
		var seq uint64
		first := true
		for k, it := range c.items {
			if first || it.seq < seq {
				oldest, seq, first = k, it.seq, false
			}
		}
		if first {
			break
		}
		c.drop(oldest)
	}
	c.seq++
	it := &cacheItem{value: value, seq: c.seq}
	if ttl > 0 {
		it.expires = now.Add(ttl)
	}
	c.items[key] = it
	c.size += n
	return nil
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	c.drop(key)
	c.mu.Unlock()
}

func (c *Cache) drop(key string) {
	if it, ok := c.items[key]; ok {
		c.size -= len(key) + len(it.value)
		delete(c.items, key)
	}
}
