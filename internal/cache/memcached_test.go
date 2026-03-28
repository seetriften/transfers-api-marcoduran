package cache_test

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"testing"
	"time"
	"transfers-api/internal/cache"
)

func testMemcached(t *testing.T) *cache.Memcached {
	t.Helper()
	addr := os.Getenv("MEMCACHED_TEST_ADDR")
	if addr == "" {
		addr = "127.0.0.1:11211"
	}
	m := cache.NewMemcached(addr, 2*time.Second)
	ctx := context.Background()
	if err := m.Set(ctx, "__cache_test_ping__", []byte("ok"), time.Second); err != nil {
		t.Skipf("memcached no disponible en %s: %v (levanta memcached o define MEMCACHED_TEST_ADDR)", addr, err)
	}
	return m
}

func TestNewMemcached(t *testing.T) {
	m := cache.NewMemcached("127.0.0.1:11211", 0)
	if m == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestMemcached_Get_CacheMiss(t *testing.T) {
	m := testMemcached(t)
	ctx := context.Background()

	key := "__cache_test_miss_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := m.Get(ctx, key)
	if err == nil {
		t.Fatal("expected error on missing key")
	}
	if err != cache.ErrCacheMiss {
		t.Fatalf("expected ErrCacheMiss, got %v", err)
	}
}

func TestMemcached_Get_ContextCanceled(t *testing.T) {
	m := testMemcached(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Get(ctx, "any")
	if err == nil || err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMemcached_Set_ContextCanceled(t *testing.T) {
	m := testMemcached(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := m.Set(ctx, "k", []byte("v"), time.Minute); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMemcached_Set_Get_Delete(t *testing.T) {
	m := testMemcached(t)
	ctx := context.Background()
	key := "__cache_test_roundtrip_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	val := []byte{1, 2, 3}

	if err := m.Set(ctx, key, val, 60*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("expected %v, got %v", val, got)
	}
	if cap(got) > 0 {
		for i := range got {
			got[i] = 0
		}
	}
	got2, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after local mutate: %v", err)
	}
	if !bytes.Equal(got2, val) {
		t.Fatal("expected original value unchanged in memcached")
	}

	if err := m.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := m.Delete(ctx, key); err != nil {
		t.Fatalf("Delete missing key should be nil, got %v", err)
	}
	_, err = m.Get(ctx, key)
	if err != cache.ErrCacheMiss {
		t.Fatalf("expected ErrCacheMiss after delete, got %v", err)
	}
}

func TestMemcached_Set_ShortTTL_MinOneSecond(t *testing.T) {
	m := testMemcached(t)
	ctx := context.Background()
	key := "__cache_test_ttl_" + strconv.FormatInt(time.Now().UnixNano(), 10)

	if err := m.Set(ctx, key, []byte("x"), 500*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	_, err := m.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
}
