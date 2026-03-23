package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"transfers-api/internal/cache"
	"transfers-api/internal/config"
	"transfers-api/internal/enums"
	"transfers-api/internal/known_errors"
	"transfers-api/internal/logging"
	"transfers-api/internal/models"
)

//go:generate mockery --name TransfersRepository --structname TransfersRepositoryMock --filename transfers_repository_mock.go --output mocks --outpkg mocks

const transferCacheTTL = 15 * time.Second

type TransfersRepository interface {
	Create(ctx context.Context, transfer models.Transfer) (string, error)
	GetByID(ctx context.Context, id string) (models.Transfer, error)
	Update(ctx context.Context, transfer models.Transfer) error
	Delete(ctx context.Context, id string) error
	ListByUserID(ctx context.Context, userID string) ([]models.Transfer, error)
}

type TransfersService struct {
	businessCfg   config.BusinessConfig
	transfersRepo TransfersRepository
	cache         cache.Cache
}

func NewTransfersService(businessCfg config.BusinessConfig, transfersRepo TransfersRepository, c cache.Cache) *TransfersService {
	return &TransfersService{
		businessCfg:   businessCfg,
		transfersRepo: transfersRepo,
		cache:         c,
	}
}

type transferCacheJSON struct {
	ID         string  `json:"id"`
	SenderID   string  `json:"sender_id"`
	ReceiverID string  `json:"receiver_id"`
	Currency   string  `json:"currency"`
	Amount     float64 `json:"amount"`
	State      string  `json:"state"`
}

func transferToCacheJSON(t models.Transfer) transferCacheJSON {
	return transferCacheJSON{
		ID:         t.ID,
		SenderID:   t.SenderID,
		ReceiverID: t.ReceiverID,
		Currency:   t.Currency.String(),
		Amount:     t.Amount,
		State:      t.State,
	}
}

func (j transferCacheJSON) toModel() models.Transfer {
	return models.Transfer{
		ID:         j.ID,
		SenderID:   j.SenderID,
		ReceiverID: j.ReceiverID,
		Currency:   enums.ParseCurrency(j.Currency),
		Amount:     j.Amount,
		State:      j.State,
	}
}

func listCacheKey(userID string) string {
	return "list:user:" + userID
}

func (s *TransfersService) invalidateTransferByID(ctx context.Context, transferID string) {
	if s.cache == nil || strings.TrimSpace(transferID) == "" {
		return
	}
	_ = s.cache.Delete(ctx, transferID)
}

func (s *TransfersService) invalidateUserLists(ctx context.Context, userIDs ...string) {
	if s.cache == nil {
		return
	}
	seen := make(map[string]struct{})
	for _, uid := range userIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		_ = s.cache.Delete(ctx, listCacheKey(uid))
	}
}

func (s *TransfersService) Create(ctx context.Context, transfer models.Transfer) (string, error) {
	if strings.TrimSpace(transfer.SenderID) == "" {
		return "", fmt.Errorf("sender_id is required: %w", known_errors.ErrBadRequest)
	}
	if strings.TrimSpace(transfer.ReceiverID) == "" {
		return "", fmt.Errorf("sender_id is required: %w", known_errors.ErrBadRequest)
	}
	if transfer.Currency == enums.CurrencyUnknown {
		return "", fmt.Errorf("invalid currency %s: %w", transfer.Currency.String(), known_errors.ErrBadRequest)
	}
	if transfer.Amount <= 0 {
		return "", fmt.Errorf("amount should be greater than 0: %w", known_errors.ErrBadRequest)
	}
	if strings.TrimSpace(transfer.State) == "" { // TODO: replace with enums.ParseState
		return "", fmt.Errorf("state is required: %w", known_errors.ErrBadRequest)
	}
	id, err := s.transfersRepo.Create(ctx, transfer)
	if err != nil {
		return "", fmt.Errorf("error creating transfer in repository: %w", err)
	}
	s.invalidateUserLists(ctx, transfer.SenderID, transfer.ReceiverID)
	return id, nil
}

func (s *TransfersService) GetByID(ctx context.Context, id string) (models.Transfer, error) {
	if s.cache != nil {
		raw, err := s.cache.Get(ctx, id)
		switch {
		case err == nil:
			var dto transferCacheJSON
			if uErr := json.Unmarshal(raw, &dto); uErr != nil {
				logging.Logger.Warnf("transfer cache BYPASS op=GetByID id=%s reason=decode err=%v", id, uErr)
			} else {
				logging.Logger.Infof("transfer cache HIT op=GetByID id=%s", id)
				return dto.toModel(), nil
			}
		case errors.Is(err, cache.ErrCacheMiss):
			logging.Logger.Infof("transfer cache MISS op=GetByID id=%s", id)
		default:
			logging.Logger.Warnf("transfer cache BYPASS op=GetByID id=%s err=%v", id, err)
		}
	}

	transfer, err := s.transfersRepo.GetByID(ctx, id)
	if err != nil {
		return models.Transfer{}, fmt.Errorf("error getting transfer %s from repository: %w", id, err)
	}

	if s.cache != nil {
		dto := transferToCacheJSON(transfer)
		if b, mErr := json.Marshal(dto); mErr == nil {
			if setErr := s.cache.Set(ctx, id, b, transferCacheTTL); setErr == nil {
				logging.Logger.Infof("transfer cache SET op=GetByID id=%s", id)
			}
		}
	}

	return transfer, nil
}

func (s *TransfersService) Update(ctx context.Context, transfer models.Transfer) error {
	if strings.TrimSpace(transfer.ID) == "" {
		return fmt.Errorf("ID is required: %w", known_errors.ErrBadRequest)
	}
	if strings.TrimSpace(transfer.SenderID) == "" &&
		strings.TrimSpace(transfer.ReceiverID) == "" &&
		transfer.Currency == enums.CurrencyUnknown &&
		transfer.Amount <= 0 &&
		strings.TrimSpace(transfer.State) == "" {
		return fmt.Errorf("error updating transfer %s: no fields to update: %w", transfer.ID, known_errors.ErrBadRequest)
	}
	old, _ := s.transfersRepo.GetByID(ctx, transfer.ID)
	if err := s.transfersRepo.Update(ctx, transfer); err != nil {
		return fmt.Errorf("error updating transfer %s in repository: %w", transfer.ID, err)
	}
	s.invalidateTransferByID(ctx, transfer.ID)
	toInvalidate := []string{old.SenderID, old.ReceiverID}
	if strings.TrimSpace(transfer.SenderID) != "" {
		toInvalidate = append(toInvalidate, transfer.SenderID)
	}
	if strings.TrimSpace(transfer.ReceiverID) != "" {
		toInvalidate = append(toInvalidate, transfer.ReceiverID)
	}
	s.invalidateUserLists(ctx, toInvalidate...)
	return nil
}

func (s *TransfersService) Delete(ctx context.Context, id string) error {
	t, err := s.transfersRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("error getting transfer %s from repository: %w", id, err)
	}
	if err := s.transfersRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("error deleting transfer %s from repository: %w", id, err)
	}
	s.invalidateTransferByID(ctx, id)
	s.invalidateUserLists(ctx, t.SenderID, t.ReceiverID)
	return nil
}

func (s *TransfersService) ListByUserID(ctx context.Context, userID string) ([]models.Transfer, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user_id is required: %w", known_errors.ErrBadRequest)
	}

	key := listCacheKey(userID)
	if s.cache != nil {
		raw, err := s.cache.Get(ctx, key)
		switch {
		case err == nil:
			var dtos []transferCacheJSON
			if uErr := json.Unmarshal(raw, &dtos); uErr != nil {
				logging.Logger.Warnf("transfer cache BYPASS op=ListByUserID user_id=%s reason=decode err=%v", userID, uErr)
			} else {
				logging.Logger.Infof("transfer cache HIT op=ListByUserID user_id=%s count=%d", userID, len(dtos))
				out := make([]models.Transfer, 0, len(dtos))
				for _, d := range dtos {
					out = append(out, d.toModel())
				}
				return out, nil
			}
		case errors.Is(err, cache.ErrCacheMiss):
			logging.Logger.Infof("transfer cache MISS op=ListByUserID user_id=%s", userID)
		default:
			logging.Logger.Warnf("transfer cache BYPASS op=ListByUserID user_id=%s err=%v", userID, err)
		}
	}

	transfers, err := s.transfersRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error listing transfers for user %s: %w", userID, err)
	}
	if transfers == nil {
		transfers = []models.Transfer{}
	}

	if s.cache != nil {
		dtos := make([]transferCacheJSON, len(transfers))
		for i, t := range transfers {
			dtos[i] = transferToCacheJSON(t)
		}
		if b, mErr := json.Marshal(dtos); mErr == nil {
			if setErr := s.cache.Set(ctx, key, b, transferCacheTTL); setErr == nil {
				logging.Logger.Infof("transfer cache SET op=ListByUserID user_id=%s count=%d", userID, len(transfers))
			}
		}
	}

	return transfers, nil
}
