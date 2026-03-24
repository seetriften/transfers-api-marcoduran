package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"transfers-api/internal/cache"
	"transfers-api/internal/config"
	"transfers-api/internal/logging"
	"transfers-api/internal/messaging"
	"transfers-api/internal/repositories"
	"transfers-api/internal/services"
	"transfers-api/internal/version"
)

// Worker: cola RabbitMQ → servicio (sin publicar de nuevo) → repositorio.
// Ejemplo didáctico: mismo dominio que la API, proceso separado.
func main() {
	logger := logging.Logger
	logger.Infof("worker starting %s@%s", version.AppName, version.Version)

	cfg := config.ParseFromEnv()
	if !cfg.RabbitMQConfig.Enabled {
		logger.Fatalf("worker requires RABBITMQ_ENABLED=true")
	}

	engine := strings.ToLower(strings.TrimSpace(cfg.StorageConfig.Engine))
	var transfersDB services.TransfersRepository
	switch engine {
	case "postgres", "postgresql":
		transfersDB = repositories.NewTransfersPostgresRepository(cfg.PostgresqlDBConfig)
		logger.Info("worker: using PostgreSQL repository")
	default:
		transfersDB = repositories.NewTransfersMongoDBRepository(cfg.MongoDBConfig)
		logger.Info("worker: using MongoDB repository")
	}

	var transfersCache cache.Cache
	if cfg.CacheConfig.Enabled {
		addr := fmt.Sprintf("%s:%d", cfg.CacheConfig.Host, cfg.CacheConfig.Port)
		transfersCache = cache.NewMemcached(addr, cfg.CacheConfig.ConnectTimeout)
		logger.Infof("worker: memcached at %s", addr)
	} else {
		logger.Info("worker: cache disabled")
	}

	svc := services.NewTransfersService(cfg.Business, transfersDB, transfersCache, nil, false)

	consumer, err := messaging.NewRabbitMQConsumer(cfg.RabbitMQConfig)
	if err != nil {
		logger.Fatalf("rabbitmq consumer: %v", err)
	}
	defer func() { _ = consumer.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Infof("worker: consuming queue=%s", cfg.RabbitMQConfig.Queue)
	err = consumer.Run(ctx, func(ctx context.Context, body []byte) error {
		return svc.ApplyFromQueue(ctx, body)
	})
	if err != nil && ctx.Err() == nil {
		logger.Fatalf("worker: %v", err)
	}
	logger.Info("worker stopped")
}
