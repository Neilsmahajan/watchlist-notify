package util

import (
	"sync"
	"time"
)

type item struct {
	v   any
	exp time.Time
}

// Cache is a simple goroutine-safe TTL cache.
type Cache struct {
	mu sync.RWMutex
	m  map[string]item
}

func NewCache() *Cache {
	return &Cache{m: make(map[string]item)}
}

func (c *Cache) Get(key string) (any, bool) {
	c.mu.RLock()
	it, ok := c.m[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !it.exp.IsZero() && time.Now().After(it.exp) {
		c.mu.Lock()
		delete(c.m, key)
		c.mu.Unlock()
		return nil, false
	}
	return it.v, true
}

func (c *Cache) Set(key string, v any, ttl time.Duration) {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	c.mu.Lock()
	c.m[key] = item{v: v, exp: exp}
	c.mu.Unlock()
}
