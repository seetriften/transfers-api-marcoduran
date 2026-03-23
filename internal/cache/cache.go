package cache

import (
	"context"
	"errors"
	"time"
)

// ErrCacheMiss: la clave no está en el backend (distinto de error de red/protocolo).
var ErrCacheMiss = errors.New("cache miss")

// Cache almacena valores opacos ([]byte); el formato lo define quien llama (p. ej. JSON en el servicio).
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
