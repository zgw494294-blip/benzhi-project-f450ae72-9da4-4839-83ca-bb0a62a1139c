package application

import "sync"

type IdempotencyCache struct {
	mu     sync.RWMutex
	values map[string]string
}

func NewIdempotencyCache() *IdempotencyCache { return &IdempotencyCache{values: map[string]string{}} }
func (c *IdempotencyCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.values[key]
	return v, ok
}
func (c *IdempotencyCache) Put(key, value string) {
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}
