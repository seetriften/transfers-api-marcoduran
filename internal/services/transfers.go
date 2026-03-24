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
	"transfers-api/internal/messaging"
	"transfers-api/internal/models"

	"github.com/google/uuid"
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
	businessCfg        config.BusinessConfig
	transfersRepo      TransfersRepository
	cache              cache.Cache
	publisher          messaging.Publisher
	eventFirstPostgres bool
}

func NewTransfersService(
	businessCfg config.BusinessConfig,
	transfersRepo TransfersRepository,
	c cache.Cache,
	pub messaging.Publisher,
	eventFirstPostgres bool,
) *TransfersService {
	return &TransfersService{
		businessCfg:        businessCfg,
		transfersRepo:      transfersRepo,
		cache:              c,
		publisher:          pub,
		eventFirstPostgres: eventFirstPostgres,
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

// publishTransferEventErr devuelve error si no hay publisher o falla marshal/publish (p. ej. fallback a BD).
func (s *TransfersService) publishTransferEventErr(ctx context.Context, eventType string, t models.Transfer) error {
	if s.publisher == nil {
		return fmt.Errorf("publisher not configured")
	}
	msg := struct {
		Type     string            `json:"type"`
		Transfer transferCacheJSON `json:"transfer"`
	}{
		Type:     eventType,
		Transfer: transferToCacheJSON(t),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", eventType, err)
	}
	if err := s.publisher.Publish(ctx, b); err != nil {
		return fmt.Errorf("publish %s: %w", eventType, err)
	}
	return nil
}

func (s *TransfersService) publishTransferEvent(ctx context.Context, eventType string, t models.Transfer) {
	if s.publisher == nil {
		return
	}
	if err := s.publishTransferEventErr(ctx, eventType, t); err != nil {
		logging.Logger.Warnf("messaging: %v", err)
	}
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
		return "", fmt.Errorf("receiver_id is required: %w", known_errors.ErrBadRequest)
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

	t := transfer
	// EVENT_FIRST_POSTGRES: publicar primero; si Rabbit falla, persistir en BD en el mismo request.
	if s.eventFirstPostgres && s.publisher != nil {
		t.ID = uuid.NewString()
		if err := s.publishTransferEventErr(ctx, messaging.EventTransferCreated, t); err != nil {
			logging.Logger.Warnf("event-first create: rabbitmq failed, persisting directly: %v", err)
			id, cerr := s.transfersRepo.Create(ctx, t)
			if cerr != nil {
				return "", fmt.Errorf("error creating transfer in repository: %w", cerr)
			}
			s.invalidateUserLists(ctx, transfer.SenderID, transfer.ReceiverID)
			return id, nil
		}
		s.invalidateUserLists(ctx, transfer.SenderID, transfer.ReceiverID)
		return t.ID, nil
	}

	id, err := s.transfersRepo.Create(ctx, t)
	if err != nil {
		return "", fmt.Errorf("error creating transfer in repository: %w", err)
	}
	s.invalidateUserLists(ctx, transfer.SenderID, transfer.ReceiverID)
	if s.publisher != nil {
		created := transfer
		created.ID = id
		s.publishTransferEvent(ctx, messaging.EventTransferCreated, created)
	}
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
	if s.eventFirstPostgres && s.publisher != nil {
		if _, err := uuid.Parse(transfer.ID); err != nil {
			return fmt.Errorf("error parsing transfer ID %s: %s: %w", transfer.ID, err.Error(), known_errors.ErrBadRequest)
		}
	}
	if strings.TrimSpace(transfer.SenderID) == "" &&
		strings.TrimSpace(transfer.ReceiverID) == "" &&
		transfer.Currency == enums.CurrencyUnknown &&
		transfer.Amount <= 0 &&
		strings.TrimSpace(transfer.State) == "" {
		return fmt.Errorf("error updating transfer %s: no fields to update: %w", transfer.ID, known_errors.ErrBadRequest)
	}

	// EVENT_FIRST_POSTGRES: intentar cola primero; si falla, actualizar en BD aquí.
	if s.eventFirstPostgres && s.publisher != nil {
		if err := s.publishTransferEventErr(ctx, messaging.EventTransferUpdated, transfer); err != nil {
			logging.Logger.Warnf("event-first update: rabbitmq failed, direct DB: %v", err)
			// continúa al flujo síncrono abajo
		} else {
			s.invalidateTransferByID(ctx, transfer.ID)
			var toInvalidate []string
			if strings.TrimSpace(transfer.SenderID) != "" {
				toInvalidate = append(toInvalidate, transfer.SenderID)
			}
			if strings.TrimSpace(transfer.ReceiverID) != "" {
				toInvalidate = append(toInvalidate, transfer.ReceiverID)
			}
			s.invalidateUserLists(ctx, toInvalidate...)
			return nil
		}
	}

	old, err := s.transfersRepo.GetByID(ctx, transfer.ID)
	if err != nil {
		return fmt.Errorf("error getting transfer %s before update: %w", transfer.ID, err)
	}

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
	// Mongo u otro modo: publicar snapshot completo tras persistir.
	if s.publisher != nil && !s.eventFirstPostgres {
		if full, gerr := s.transfersRepo.GetByID(ctx, transfer.ID); gerr == nil {
			s.publishTransferEvent(ctx, messaging.EventTransferUpdated, full)
		}
	}
	return nil
}

func (s *TransfersService) Delete(ctx context.Context, id string) error {
	t, err := s.transfersRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("error getting transfer %s from repository: %w", id, err)
	}

	if s.eventFirstPostgres && s.publisher != nil {
		if err := s.publishTransferEventErr(ctx, messaging.EventTransferDeleted, t); err != nil {
			logging.Logger.Warnf("event-first delete: rabbitmq failed, direct DB: %v", err)
			// flujo síncrono: borrar en BD
		} else {
			s.invalidateTransferByID(ctx, id)
			s.invalidateUserLists(ctx, t.SenderID, t.ReceiverID)
			return nil
		}
	}

	if err := s.transfersRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("error deleting transfer %s from repository: %w", id, err)
	}
	s.invalidateTransferByID(ctx, id)
	s.invalidateUserLists(ctx, t.SenderID, t.ReceiverID)
	if s.publisher != nil && !s.eventFirstPostgres {
		s.publishTransferEvent(ctx, messaging.EventTransferDeleted, t)
	}
	return nil
}

// ApplyFromQueue aplica el mensaje JSON de la cola solo en repositorio (sin volver a publicar). Uso: worker.
func (s *TransfersService) ApplyFromQueue(ctx context.Context, body []byte) error {
	var msg struct {
		Type     string            `json:"type"`
		Transfer transferCacheJSON `json:"transfer"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return fmt.Errorf("decode queue message: %w", err)
	}
	tr := msg.Transfer.toModel()
	switch msg.Type {
	case messaging.EventTransferCreated:
		_, err := s.transfersRepo.Create(ctx, tr)
		return err
	case messaging.EventTransferUpdated:
		return s.transfersRepo.Update(ctx, tr)
	case messaging.EventTransferDeleted:
		return s.transfersRepo.Delete(ctx, tr.ID)
	default:
		logging.Logger.Warnf("unknown transfer event type from queue: %q", msg.Type)
		return nil
	}
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
