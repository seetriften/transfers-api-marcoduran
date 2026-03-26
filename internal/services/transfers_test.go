package services_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"transfers-api/internal/cache"
	"transfers-api/internal/config"
	"transfers-api/internal/enums"
	"transfers-api/internal/known_errors"
	"transfers-api/internal/models"
	"transfers-api/internal/services"
)

type fakeRepo struct {
	createID        string
	createErr       error
	createCalls     int
	lastCreated     models.Transfer
	store           map[string]models.Transfer
	getByIDErr      error
	updateErr       error
	updateCalls     int
	lastUpdated     models.Transfer
	deleteErr       error
	deleteCalls     int
	lastDeletedID   string
	listByUserIDErr error
}

func (f *fakeRepo) Create(_ context.Context, transfer models.Transfer) (string, error) {
	f.createCalls++
	f.lastCreated = transfer
	if f.createErr != nil {
		return "", f.createErr
	}
	id := f.createID
	if id == "" {
		id = transfer.ID
	}
	if id == "" {
		id = "created-id"
	}
	transfer.ID = id
	if f.store == nil {
		f.store = map[string]models.Transfer{}
	}
	f.store[id] = transfer
	return id, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id string) (models.Transfer, error) {
	if f.getByIDErr != nil {
		return models.Transfer{}, f.getByIDErr
	}
	if f.store == nil {
		return models.Transfer{}, known_errors.ErrNotFound
	}
	t, ok := f.store[id]
	if !ok {
		return models.Transfer{}, known_errors.ErrNotFound
	}
	return t, nil
}

func (f *fakeRepo) Update(_ context.Context, transfer models.Transfer) error {
	f.updateCalls++
	f.lastUpdated = transfer
	if f.updateErr != nil {
		return f.updateErr
	}
	if f.store == nil {
		f.store = map[string]models.Transfer{}
	}
	curr, ok := f.store[transfer.ID]
	if !ok {
		return known_errors.ErrNotFound
	}
	if transfer.SenderID != "" {
		curr.SenderID = transfer.SenderID
	}
	if transfer.ReceiverID != "" {
		curr.ReceiverID = transfer.ReceiverID
	}
	if transfer.Currency != enums.CurrencyUnknown {
		curr.Currency = transfer.Currency
	}
	if transfer.Amount > 0 {
		curr.Amount = transfer.Amount
	}
	if transfer.State != "" {
		curr.State = transfer.State
	}
	f.store[transfer.ID] = curr
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id string) error {
	f.deleteCalls++
	f.lastDeletedID = id
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if f.store != nil {
		delete(f.store, id)
	}
	return nil
}

func (f *fakeRepo) ListByUserID(_ context.Context, userID string) ([]models.Transfer, error) {
	if f.listByUserIDErr != nil {
		return nil, f.listByUserIDErr
	}
	out := make([]models.Transfer, 0)
	for _, t := range f.store {
		if t.SenderID == userID || t.ReceiverID == userID {
			out = append(out, t)
		}
	}
	return out, nil
}

type fakeCache struct {
	items map[string][]byte
}

func (f *fakeCache) Get(_ context.Context, key string) ([]byte, error) {
	if f.items == nil {
		return nil, cache.ErrCacheMiss
	}
	b, ok := f.items[key]
	if !ok {
		return nil, cache.ErrCacheMiss
	}
	return b, nil
}

func (f *fakeCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	if f.items == nil {
		f.items = map[string][]byte{}
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	f.items[key] = cp
	return nil
}

func (f *fakeCache) Delete(_ context.Context, key string) error {
	if f.items != nil {
		delete(f.items, key)
	}
	return nil
}

func TestTransfersService_Create_Valid(t *testing.T) {
	repo := &fakeRepo{createID: "id-1"}
	svc := services.NewTransfersService(config.BusinessConfig{}, repo, &fakeCache{}, nil, false)

	id, err := svc.Create(context.Background(), models.Transfer{
		SenderID:   "u1",
		ReceiverID: "u2",
		Currency:   enums.CurrencyUSD,
		Amount:     100,
		State:      "created",
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != "id-1" {
		t.Fatalf("expected id-1, got %s", id)
	}
	if repo.createCalls != 1 {
		t.Fatalf("expected 1 create call, got %d", repo.createCalls)
	}
}

func TestTransfersService_Create_InvalidAmount(t *testing.T) {
	repo := &fakeRepo{}
	svc := services.NewTransfersService(config.BusinessConfig{}, repo, nil, nil, false)

	_, err := svc.Create(context.Background(), models.Transfer{
		SenderID:   "u1",
		ReceiverID: "u2",
		Currency:   enums.CurrencyUSD,
		Amount:     0,
		State:      "created",
	})

	if err == nil {
		t.Fatal("expected error for invalid amount")
	}
	if repo.createCalls != 0 {
		t.Fatalf("expected 0 create calls, got %d", repo.createCalls)
	}
}

func TestTransfersService_GetByID_UsesCache(t *testing.T) {
	repo := &fakeRepo{
		store: map[string]models.Transfer{
			"id-1": {
				ID:         "id-1",
				SenderID:   "u1",
				ReceiverID: "u2",
				Currency:   enums.CurrencyUSD,
				Amount:     50,
				State:      "ok",
			},
		},
	}
	c := &fakeCache{}
	svc := services.NewTransfersService(config.BusinessConfig{}, repo, c, nil, false)

	first, err := svc.GetByID(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if first.ID != "id-1" {
		t.Fatalf("expected id-1, got %s", first.ID)
	}

	repo.getByIDErr = errors.New("repo should not be used when cache hits")
	second, err := svc.GetByID(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("expected no error on cache hit, got %v", err)
	}
	if second.ID != "id-1" {
		t.Fatalf("expected id-1 on cache hit, got %s", second.ID)
	}
}

func TestTransfersService_Update_Basic(t *testing.T) {
	repo := &fakeRepo{
		store: map[string]models.Transfer{
			"id-1": {
				ID:         "id-1",
				SenderID:   "u1",
				ReceiverID: "u2",
				Currency:   enums.CurrencyUSD,
				Amount:     10,
				State:      "pending",
			},
		},
	}
	svc := services.NewTransfersService(config.BusinessConfig{}, repo, nil, nil, false)

	err := svc.Update(context.Background(), models.Transfer{
		ID:     "id-1",
		Amount: 20,
		State:  "completed",
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("expected 1 update call, got %d", repo.updateCalls)
	}
	updated, gErr := repo.GetByID(context.Background(), "id-1")
	if gErr != nil {
		t.Fatalf("expected to find updated transfer, got %v", gErr)
	}
	if updated.Amount != 20.0 {
		t.Fatalf("expected amount 20.0, got %v", updated.Amount)
	}
	if updated.State != "completed" {
		t.Fatalf("expected state completed, got %s", updated.State)
	}
}

func TestTransfersService_Delete_Basic(t *testing.T) {
	repo := &fakeRepo{
		store: map[string]models.Transfer{
			"id-1": {
				ID:         "id-1",
				SenderID:   "u1",
				ReceiverID: "u2",
				Currency:   enums.CurrencyUSD,
				Amount:     10,
				State:      "pending",
			},
		},
	}
	svc := services.NewTransfersService(config.BusinessConfig{}, repo, nil, nil, false)

	err := svc.Delete(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo.deleteCalls != 1 {
		t.Fatalf("expected 1 delete call, got %d", repo.deleteCalls)
	}
	_, gErr := repo.GetByID(context.Background(), "id-1")
	if gErr == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestTransfersService_ListByUserID_Basic(t *testing.T) {
	repo := &fakeRepo{
		store: map[string]models.Transfer{
			"id-1": {
				ID:         "id-1",
				SenderID:   "u1",
				ReceiverID: "u2",
				Currency:   enums.CurrencyUSD,
				Amount:     10,
				State:      "pending",
			},
			"id-2": {
				ID:         "id-2",
				SenderID:   "u3",
				ReceiverID: "u1",
				Currency:   enums.CurrencyEUR,
				Amount:     20,
				State:      "done",
			},
		},
	}
	svc := services.NewTransfersService(config.BusinessConfig{}, repo, &fakeCache{}, nil, false)

	list, err := svc.ListByUserID(context.Background(), "u1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 transfers, got %d", len(list))
	}
}