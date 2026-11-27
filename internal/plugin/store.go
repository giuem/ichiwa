package plugin

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Limits of host.storage and host.cache (§8).
const (
	MaxStorageBytes   = 64 << 10
	MaxCacheBytes     = 4 << 20
	MaxCacheItemBytes = 256 << 10
	// MaxCacheItems bounds the items of a cache, each of which counts
	// cacheItemOverhead bytes besides its key and value, so that the
	// work of a write stays small however small its items.
	MaxCacheItems     = 4096
	cacheItemOverhead = 64
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
	// A string the plugin made may share the memory of a much larger
	// one, which the storage would keep alive: it keeps a copy.
	key, value = strings.Clone(key), strings.Clone(value)
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

// Cache is a plugin's in-memory cache: MaxCacheBytes and MaxCacheItems
// in all, MaxCacheItemBytes an item, the oldest written dropped first.
type Cache struct {
	mu    sync.Mutex
	items map[string]*cacheItem
	order list.List // of *cacheItem, the oldest written first
	size  int
	// soonest is no later than when the first item expires, zero when
	// none does: until then no item has to be looked for to drop.
	soonest time.Time
	now     func() time.Time
}

type cacheItem struct {
	key     string
	value   string
	expires time.Time
	elem    *list.Element
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
	c.setLocked(key, value, ttl)
	return nil
}

// Add stores value like Set, but only when key holds nothing or only an
// expired value, and reports whether it did: one call among many running
// at once wins, which is how a plugin locks between its calls.
func (c *Cache) Add(key, value string, ttl time.Duration) (bool, error) {
	n := len(key) + len(value)
	if n > MaxCacheItemBytes {
		return false, fmt.Errorf("host.cache 单项不能超过 %d KiB", MaxCacheItemBytes>>10)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if it, ok := c.items[key]; ok && (it.expires.IsZero() || c.now().Before(it.expires)) {
		return false, nil
	}
	c.setLocked(key, value, ttl)
	return true, nil
}

// itemSize is what an item counts towards MaxCacheBytes.
func itemSize(key, value string) int { return len(key) + len(value) + cacheItemOverhead }

func (c *Cache) setLocked(key, value string, ttl time.Duration) {
	n := itemSize(key, value)
	c.drop(key)
	now := c.now()
	if c.full(n) && !c.soonest.IsZero() && !now.Before(c.soonest) {
		c.dropExpired(now)
	}
	for c.full(n) && c.order.Len() > 0 {
		c.drop(c.order.Front().Value.(*cacheItem).key)
	}
	// A copy, as for Storage.Set.
	it := &cacheItem{key: strings.Clone(key), value: strings.Clone(value)}
	if ttl > 0 {
		it.expires = now.Add(ttl)
		if c.soonest.IsZero() || it.expires.Before(c.soonest) {
			c.soonest = it.expires
		}
	}
	it.elem = c.order.PushBack(it)
	c.items[it.key] = it
	c.size += n
}

// full tells whether an item of n bytes needs room made for it.
func (c *Cache) full(n int) bool {
	return c.size+n > MaxCacheBytes || len(c.items) >= MaxCacheItems
}

// dropExpired drops the items expired at now and notes when the next
// expires.
func (c *Cache) dropExpired(now time.Time) {
	c.soonest = time.Time{}
	for k, it := range c.items {
		switch {
		case it.expires.IsZero():
		case !now.Before(it.expires):
			c.drop(k)
		case c.soonest.IsZero() || it.expires.Before(c.soonest):
			c.soonest = it.expires
		}
	}
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	c.drop(key)
	c.mu.Unlock()
}

func (c *Cache) drop(key string) {
	if it, ok := c.items[key]; ok {
		c.size -= itemSize(key, it.value)
		c.order.Remove(it.elem)
		delete(c.items, key)
	}
}

// savedItem is a cache item as Save writes it.
type savedItem struct {
	Key     string    `json:"k"`
	Value   string    `json:"v"`
	Expires time.Time `json:"e,omitzero"`
}

// Save writes the cache's live items to path, oldest first, so that a
// later LoadCache has them back with what is left of their time: the
// command-line tool keeps a plugin's cache between its runs this way.
func (c *Cache) Save(path string) error {
	c.mu.Lock()
	now := c.now()
	items := make([]savedItem, 0, len(c.items))
	for e := c.order.Front(); e != nil; e = e.Next() {
		it := e.Value.(*cacheItem)
		if !it.expires.IsZero() && !now.Before(it.expires) {
			continue
		}
		items = append(items, savedItem{Key: it.key, Value: it.value, Expires: it.expires})
	}
	c.mu.Unlock()
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadCache reads a cache Save wrote; a missing file is an empty cache.
func LoadCache(path string) (*Cache, error) {
	c := NewCache()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	var items []savedItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("%s 不是 host.cache 的存档：%w", path, err)
	}
	now := c.now()
	for _, it := range items {
		var ttl time.Duration
		if !it.Expires.IsZero() {
			if ttl = it.Expires.Sub(now); ttl <= 0 {
				continue
			}
		}
		if err := c.Set(it.Key, it.Value, ttl); err != nil {
			return nil, err
		}
	}
	return c, nil
}
