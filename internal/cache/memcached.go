package cache

import (
	"bytes"
	"context"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

type Memcached struct {
	client *memcache.Client
}

func NewMemcached(address string, opTimeout time.Duration) *Memcached {
	c := memcache.New(address)
	if opTimeout > 0 {
		c.Timeout = opTimeout
	}
	return &Memcached{client: c}
}

func (m *Memcached) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item, err := m.client.Get(key)
	if err == memcache.ErrCacheMiss {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	return bytes.Clone(item.Value), nil
}

func (m *Memcached) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sec := int32(ttl.Seconds())
	if ttl > 0 && sec < 1 {
		sec = 1
	}
	return m.client.Set(&memcache.Item{
		Key:        key,
		Value:      value,
		Expiration: sec,
	})
}

func (m *Memcached) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := m.client.Delete(key)
	if err == memcache.ErrCacheMiss {
		return nil
	}
	return err
}
