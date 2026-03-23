package main

import (
	"fmt"
	"strings"

	"transfers-api/internal/cache"
	"transfers-api/internal/config"
	"transfers-api/internal/handlers"
	"transfers-api/internal/logging"
	"transfers-api/internal/repositories"
	"transfers-api/internal/services"
	"transfers-api/internal/transport"
	"transfers-api/internal/version"
)

func main() {
	// init logger
	logger := logging.Logger
	logger.Info("logger started")

	// init config
	cfg := config.ParseFromEnv()
	logger.Infof("config loaded: %v", cfg.String())

	// init repositories
	var transfersDB services.TransfersRepository
	switch strings.ToLower(strings.TrimSpace(cfg.StorageConfig.Engine)) {
	case "postgres", "postgresql":
		transfersDB = repositories.NewTransfersPostgresRepository(cfg.PostgresqlDBConfig)
		logger.Info("using PostgreSQL repository")
	default:
		transfersDB = repositories.NewTransfersMongoDBRepository(cfg.MongoDBConfig)
		logger.Info("using MongoDB repository")
	}
	logger.Info("repositories created")

	var transfersCache cache.Cache
	if cfg.CacheConfig.Enabled {
		addr := fmt.Sprintf("%s:%d", cfg.CacheConfig.Host, cfg.CacheConfig.Port)
		transfersCache = cache.NewMemcached(addr, cfg.CacheConfig.ConnectTimeout)
		logger.Infof("cache enabled (memcached at %s)", addr)
	} else {
		logger.Info("cache disabled (set CACHE_ENABLED=true to use Memcached)")
	}

	// init services
	transfersService := services.NewTransfersService(cfg.Business, transfersDB, transfersCache)
	logger.Infof("services created")

	// init handlers
	transfersHandler := handlers.NewTransfersHandler(transfersService)
	logger.Infof("handlers created")

	// init server
	server := transport.NewHTTPServer(transfersHandler)
	server.MapRoutes()
	logger.Infof("server created, running %s@%s", version.AppName, version.Version)

	// run server
	server.Run(":8080")
}
