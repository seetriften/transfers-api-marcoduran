package messaging

import "context"

type Publisher interface {
	Publish(ctx context.Context, body []byte) error
	Close() error
}
