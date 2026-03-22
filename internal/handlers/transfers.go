package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"transfers-api/internal/enums"
	"transfers-api/internal/known_errors"
	"transfers-api/internal/models"

	"github.com/gin-gonic/gin"
)

//go:generate mockery --name TransfersService --structname TransfersServiceMock --filename transfers_service_mock.go --output mocks --outpkg mocks

type TransfersService interface {
	Create(ctx context.Context, transfer models.Transfer) (string, error)
	GetByID(ctx context.Context, id string) (models.Transfer, error)
	Update(ctx context.Context, transfer models.Transfer) error
	Delete(ctx context.Context, id string) error
	ListByUserID(ctx context.Context, userID string) ([]models.Transfer, error)
}

type TransfersHandler struct {
	transfersSvc TransfersService
}

func NewTransfersHandler(transfersSvc TransfersService) *TransfersHandler {
	return &TransfersHandler{
		transfersSvc: transfersSvc,
	}
}

type CreateTransferRequest struct {
	SenderID   string  `json:"sender_id"`
	ReceiverID string  `json:"receiver_id"`
	Currency   string  `json:"currency"`
	Amount     float64 `json:"amount"`
	State      string  `json:"state"`
}

func (h *TransfersHandler) Create(ctx *gin.Context) {
	// parse request
	var request CreateTransferRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	currency := enums.ParseCurrency(request.Currency)
	if currency == enums.CurrencyUnknown {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid currency: %s", request.Currency)})
		return
	}

	// create transfer
	id, err := h.transfersSvc.Create(ctx.Request.Context(), models.Transfer{
		SenderID:   request.SenderID,
		ReceiverID: request.ReceiverID,
		Currency:   enums.ParseCurrency(request.Currency),
		Amount:     request.Amount,
		State:      request.State, // TODO: replace with enums.ParseState
	})
	if err != nil {
		if errors.Is(err, known_errors.ErrBadRequest) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// return created
	ctx.JSON(http.StatusCreated, gin.H{"id": id})
}

type GetTransferByIDResponse struct {
	ID         string  `json:"id"`
	SenderID   string  `json:"sender_id"`
	ReceiverID string  `json:"receiver_id"`
	Currency   string  `json:"currency"`
	Amount     float64 `json:"amount"`
	State      string  `json:"state"`
}

func (h *TransfersHandler) GetByID(ctx *gin.Context) {
	// parse id
	id := ctx.Param("id")

	// get transfer
	transfer, err := h.transfersSvc.GetByID(ctx.Request.Context(), id)
	if err != nil {
		if errors.Is(err, known_errors.ErrBadRequest) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, known_errors.ErrNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// return transfer
	ctx.JSON(http.StatusOK, GetTransferByIDResponse{
		ID:         transfer.ID,
		SenderID:   transfer.SenderID,
		ReceiverID: transfer.ReceiverID,
		Currency:   transfer.Currency.String(),
		Amount:     transfer.Amount,
		State:      transfer.State, // TODO: replace with transfer.State.String()
	})
}

type UpdateTransferRequest struct {
	// we can apply support for custom fields only if needed
	SenderID   string  `json:"sender_id"`
	ReceiverID string  `json:"receiver_id"`
	Currency   string  `json:"currency"`
	Amount     float64 `json:"amount"`
	State      string  `json:"state"`
}

func (h *TransfersHandler) Update(ctx *gin.Context) {
	// parse id
	id := ctx.Param("id")

	// parse request
	var request UpdateTransferRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	currency := enums.ParseCurrency(request.Currency)
	if strings.TrimSpace(request.Currency) != "" && currency == enums.CurrencyUnknown {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid currency: %s", request.Currency)})
		return
	}

	// update transfer
	if err := h.transfersSvc.Update(ctx.Request.Context(), models.Transfer{
		ID:         id,
		SenderID:   request.SenderID,
		ReceiverID: request.ReceiverID,
		Currency:   enums.ParseCurrency(request.Currency),
		Amount:     request.Amount,
		State:      request.State, // TODO: replace with enums.ParseState
	}); err != nil {
		if errors.Is(err, known_errors.ErrBadRequest) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, known_errors.ErrNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// return ok
	ctx.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *TransfersHandler) Delete(ctx *gin.Context) {
	// parse id
	id := ctx.Param("id")

	// delete transfer
	if err := h.transfersSvc.Delete(ctx.Request.Context(), id); err != nil {
		if errors.Is(err, known_errors.ErrNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// return ok
	ctx.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *TransfersHandler) ListByUserID(ctx *gin.Context) {
	userID := ctx.Param("user_id")
	transfers, err := h.transfersSvc.ListByUserID(ctx.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, known_errors.ErrBadRequest) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]GetTransferByIDResponse, 0, len(transfers))
	for _, t := range transfers {
		out = append(out, GetTransferByIDResponse{
			ID:         t.ID,
			SenderID:   t.SenderID,
			ReceiverID: t.ReceiverID,
			Currency:   t.Currency.String(),
			Amount:     t.Amount,
			State:      t.State,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"transfers": out})
}
