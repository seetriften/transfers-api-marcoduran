package messaging

import (
	"net/url"
	"strings"
	"testing"
	"transfers-api/internal/config"
)

func TestAmqpURI_Errors(t *testing.T) {
	_, err := amqpURI(config.RabbitMQ{Hostname: "", Port: 5672})
	if err == nil {
		t.Fatal("expected error for empty hostname")
	}
	if !strings.Contains(err.Error(), "hostname") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = amqpURI(config.RabbitMQ{Hostname: "localhost", Port: 0})
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAmqpURI_Values(t *testing.T) {
	uStr, err := amqpURI(config.RabbitMQ{
		Hostname: "rabbit.example",
		Port:     5672,
		Username: "guest",
		Password: "guest",
		VHost:    "/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	uri, err := url.Parse(uStr)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if uri.Scheme != "amqp" {
		t.Fatalf("scheme: %q", uri.Scheme)
	}
	if uri.Host != "rabbit.example:5672" {
		t.Fatalf("host: %q", uri.Host)
	}
	if uri.Path != "/" {
		t.Fatalf("path for default vhost: %q", uri.Path)
	}
	pass, _ := uri.User.Password()
	if uri.User.Username() != "guest" || pass != "guest" {
		t.Fatalf("userinfo: %v", uri.User)
	}

	uStr, err = amqpURI(config.RabbitMQ{
		Hostname: "localhost",
		Port:     5672,
		Username: "u",
		Password: "p",
		VHost:    "/myvhost",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	uri, err = url.Parse(uStr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if uri.Path != "/myvhost" {
		t.Fatalf("path for custom vhost: %q", uri.Path)
	}
}
