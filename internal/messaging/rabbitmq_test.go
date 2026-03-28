package messaging_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"transfers-api/internal/config"
	"transfers-api/internal/messaging"
)

func testRabbitConfig(t *testing.T) config.RabbitMQ {
	t.Helper()
	host := os.Getenv("RABBITMQ_TEST_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := 5672
	if p := os.Getenv("RABBITMQ_TEST_PORT"); p != "" {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			t.Fatalf("RABBITMQ_TEST_PORT: %v", err)
		}
	}
	queue := os.Getenv("RABBITMQ_TEST_QUEUE")
	if queue == "" {
		queue = "messaging.test." + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return config.RabbitMQ{
		Hostname:       host,
		Port:           port,
		Username:       getenvRabbitDefault("RABBITMQ_TEST_USER", "guest"),
		Password:       getenvRabbitDefault("RABBITMQ_TEST_PASS", "guest"),
		VHost:          getenvRabbitDefault("RABBITMQ_TEST_VHOST", "/"),
		Queue:          queue,
		ConnectTimeout: 3 * time.Second,
	}
}

func getenvRabbitDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestRabbitMQ_PublishAndConsume(t *testing.T) {
	cfg := testRabbitConfig(t)

	pub, err := messaging.NewRabbitMQPublisher(cfg)
	if err != nil {
		t.Skipf("rabbitmq no disponible: %v (levanta Rabbit o define RABBITMQ_TEST_*)", err)
	}
	defer func() { _ = pub.Close() }()

	cons, err := messaging.NewRabbitMQConsumer(cfg)
	if err != nil {
		t.Fatalf("rabbitmq consumer: %v", err)
	}
	defer func() { _ = cons.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- cons.Run(ctx, func(_ context.Context, body []byte) error {
			received <- append([]byte(nil), body...)
			return nil
		})
	}()

	time.Sleep(150 * time.Millisecond)

	want := []byte(`{"type":"transfer.created","transfer":{"id":"x"}}`)
	if err := pub.Publish(context.Background(), want); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if string(got) != string(want) {
			t.Fatalf("body mismatch:\nwant %s\ngot  %s", want, got)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timeout esperando mensaje")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("consumer Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("el consumer no terminó tras cancelar el contexto")
	}
}

func TestRabbitMQ_EmptyQueueRejected(t *testing.T) {
	cfg := testRabbitConfig(t)
	cfg.Queue = ""
	pub, err := messaging.NewRabbitMQPublisher(cfg)
	if pub != nil {
		_ = pub.Close()
	}
	if err == nil {
		t.Fatal("se esperaba error con queue vacía")
	}
	if strings.Contains(err.Error(), "dial") {
		t.Skipf("rabbitmq no disponible: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "queue") {
		t.Fatalf("se esperaba error de queue, obtuvo: %v", err)
	}
}
