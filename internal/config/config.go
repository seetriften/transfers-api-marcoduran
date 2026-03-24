package config

import (
	"encoding/json"
	"time"
	"transfers-api/internal/logging"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	Business           BusinessConfig `json:"business"`
	StorageConfig      Storage        `json:"storage"`
	MongoDBConfig      MongoDB        `json:"mongodb"`
	PostgresqlDBConfig PostgresqlDB   `json:"postgres"`
	CacheConfig        Cache          `json:"cache"`
	RabbitMQConfig     RabbitMQ       `json:"rabbitmq"`
	EventFirstPostgres bool           `env:"EVENT_FIRST_POSTGRES" envDefault:"false" json:"event_first_postgres"`
}

type BusinessConfig struct {
	TransferMinAmount int `env:"TRANSFER_MIN_AMOUNT" envDefault:"1" json:"transfer_min_amount"`
}

type Storage struct {
	Engine string `env:"STORAGE_ENGINE" envDefault:"mongodb" json:"engine"`
}

type MongoDB struct {
	ConnectTimeout time.Duration `env:"MONGODB_CONNECT_TIMEOUT" envDefault:"10s" json:"connect_timeout"`
	Hostname       string        `env:"MONGODB_HOSTNAME" envDefault:"mongodb" json:"hostname"`
	Port           int           `env:"MONGODB_PORT" envDefault:"27017" json:"port"`
	Username       string        `env:"MONGODB_USERNAME" envDefault:"root" json:"username"`
	Password       string        `env:"MONGODB_PASSWORD" envDefault:"root" json:"password"`
	Database       string        `env:"MONGODB_DATABASE" envDefault:"transfers-db" json:"database"`
	Collection     string        `env:"MONGODB_COLLECTION" envDefault:"transfers" json:"collection"`
}

type PostgresqlDB struct {
	ConnectTimeout time.Duration `env:"POSTGRES_CONNECT_TIMEOUT" envDefault:"10s" json:"connect_timeout"`
	Hostname       string        `env:"POSTGRES_HOSTNAME" envDefault:"postgres" json:"hostname"`
	Port           int           `env:"POSTGRES_PORT" envDefault:"5432" json:"port"`
	Username       string        `env:"POSTGRES_USERNAME" envDefault:"postgres" json:"username"`
	Password       string        `env:"POSTGRES_PASSWORD" envDefault:"postgres" json:"password"`
	Database       string        `env:"POSTGRES_DATABASE" envDefault:"transfers_db" json:"database"`
	SSLMode        string        `env:"POSTGRES_SSL_MODE" envDefault:"disable" json:"ssl_mode"`
}

type Cache struct {
	ConnectTimeout time.Duration `env:"CACHE_CONNECT_TIMEOUT" envDefault:"10s" json:"connect_timeout"`
	Enabled        bool          `env:"CACHE_ENABLED" envDefault:"false" json:"enabled"`
	Host           string        `env:"CACHE_HOST" envDefault:"localhost" json:"host"`
	Port           int           `env:"CACHE_PORT" envDefault:"11211" json:"port"`
}

type RabbitMQ struct {
	ConnectTimeout time.Duration `env:"RABBITMQ_CONNECT_TIMEOUT" envDefault:"10s" json:"connect_timeout"`
	Enabled        bool          `env:"RABBITMQ_ENABLED" envDefault:"false" json:"enabled"`
	Hostname       string        `env:"RABBITMQ_HOSTNAME" envDefault:"localhost" json:"hostname"`
	Port           int           `env:"RABBITMQ_PORT" envDefault:"5672" json:"port"`
	Username       string        `env:"RABBITMQ_USERNAME" envDefault:"guest" json:"username"`
	Password       string        `env:"RABBITMQ_PASSWORD" envDefault:"guest" json:"password"`
	VHost          string        `env:"RABBITMQ_VHOST" envDefault:"/" json:"vhost"`
	Queue          string        `env:"RABBITMQ_QUEUE" envDefault:"transfers.events" json:"queue"`
}

func ParseFromEnv() *Config {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		logging.Logger.Fatalf("error parsing config: %v", err)
	}
	return &cfg
}

func ParseFromJSON(input []byte) *Config {
	var cfg Config
	if err := json.Unmarshal(input, &cfg); err != nil {
		logging.Logger.Fatalf("error parsing config: %v", err)
	}
	return &cfg
}

func (c *Config) String() string {
	bytes, err := json.Marshal(c)
	if err != nil {
		logging.Logger.Fatalf("error marshaling config: %v", err)
	}
	return string(bytes)
}
