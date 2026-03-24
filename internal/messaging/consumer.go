package messaging

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"transfers-api/internal/config"
	"transfers-api/internal/logging"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQConsumer struct {
	conn  *amqp.Connection
	ch    *amqp.Channel
	queue string
}

func NewRabbitMQConsumer(cfg config.RabbitMQ) (*RabbitMQConsumer, error) {
	uri, err := amqpURI(cfg)
	if err != nil {
		return nil, err
	}
	dialTimeout := cfg.ConnectTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := amqp.DialConfig(uri, amqp.Config{Dial: dialer.Dial})
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq channel: %w", err)
	}
	q := strings.TrimSpace(cfg.Queue)
	if q == "" {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq: queue name is required")
	}
	_, err = ch.QueueDeclare(
		q,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq queue declare: %w", err)
	}
	prefetch := 10
	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq qos: %w", err)
	}
	return &RabbitMQConsumer{conn: conn, ch: ch, queue: q}, nil
}

// Run bloquea hasta que ctx se cancela. handler debe devolver error solo si el mensaje debe reintentarse.
func (c *RabbitMQConsumer) Run(ctx context.Context, handler func(context.Context, []byte) error) error {
	msgs, err := c.ch.Consume(
		c.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("rabbitmq consume: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-msgs:
			if !ok {
				return nil
			}
			if err := handler(ctx, d.Body); err != nil {
				logging.Logger.Warnf("worker: message handler error (nack requeue): %v", err)
				if nackErr := d.Nack(false, true); nackErr != nil {
					logging.Logger.Warnf("worker: nack: %v", nackErr)
				}
				continue
			}
			if ackErr := d.Ack(false); ackErr != nil {
				logging.Logger.Warnf("worker: ack: %v", ackErr)
			}
		}
	}
}

func (c *RabbitMQConsumer) Close() error {
	var err error
	if c.ch != nil {
		if e := c.ch.Close(); e != nil {
			err = e
		}
		c.ch = nil
	}
	if c.conn != nil {
		if e := c.conn.Close(); e != nil {
			err = e
		}
		c.conn = nil
	}
	return err
}
