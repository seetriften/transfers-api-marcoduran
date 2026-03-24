package messaging

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"transfers-api/internal/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQPublisher struct {
	conn  *amqp.Connection
	ch    *amqp.Channel
	queue string
	mu    sync.Mutex
}

func NewRabbitMQPublisher(cfg config.RabbitMQ) (*RabbitMQPublisher, error) {
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
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq queue declare: %w", err)
	}
	return &RabbitMQPublisher{conn: conn, ch: ch, queue: q}, nil
}

func amqpURI(cfg config.RabbitMQ) (string, error) {
	host := strings.TrimSpace(cfg.Hostname)
	if host == "" {
		return "", fmt.Errorf("rabbitmq: hostname is required")
	}
	if cfg.Port <= 0 {
		return "", fmt.Errorf("rabbitmq: invalid port")
	}
	u := url.URL{
		Scheme: "amqp",
		Host:   fmt.Sprintf("%s:%d", host, cfg.Port),
		User:   url.UserPassword(cfg.Username, cfg.Password),
	}
	vh := cfg.VHost
	if vh == "" {
		vh = "/"
	}
	if vh == "/" {
		u.Path = "/"
	} else {
		u.Path = "/" + strings.TrimPrefix(vh, "/")
	}
	return u.String(), nil
}

func (p *RabbitMQPublisher) Publish(ctx context.Context, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch == nil {
		return fmt.Errorf("rabbitmq: channel closed")
	}
	return p.ch.PublishWithContext(ctx,
		"",
		p.queue,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
}

func (p *RabbitMQPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var errs []error
	if p.ch != nil {
		if err := p.ch.Close(); err != nil {
			errs = append(errs, err)
		}
		p.ch = nil
	}
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			errs = append(errs, err)
		}
		p.conn = nil
	}
	return errors.Join(errs...)
}
