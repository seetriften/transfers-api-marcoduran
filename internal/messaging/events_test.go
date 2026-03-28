package messaging_test

import (
	"testing"
	"transfers-api/internal/messaging"
)

func TestEventConstants(t *testing.T) {
	if messaging.EventTransferCreated != "transfer.created" {
		t.Fatalf("EventTransferCreated: %q", messaging.EventTransferCreated)
	}
	if messaging.EventTransferUpdated != "transfer.updated" {
		t.Fatalf("EventTransferUpdated: %q", messaging.EventTransferUpdated)
	}
	if messaging.EventTransferDeleted != "transfer.deleted" {
		t.Fatalf("EventTransferDeleted: %q", messaging.EventTransferDeleted)
	}
}
