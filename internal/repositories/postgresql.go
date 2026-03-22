package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"transfers-api/internal/config"
	"transfers-api/internal/enums"
	"transfers-api/internal/known_errors"
	"transfers-api/internal/logging"
	"transfers-api/internal/models"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type TransfersPostgresRepo struct {
	db *sql.DB
}

func NewTransfersPostgresRepository(cfg config.PostgresqlDB) *TransfersPostgresRepo {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=%d",
		cfg.Hostname,
		cfg.Port,
		cfg.Username,
		cfg.Password,
		cfg.Database,
		cfg.SSLMode,
		int(cfg.ConnectTimeout.Seconds()),
	)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		logging.Logger.Fatalf("error opening PostgreSQL connection: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		logging.Logger.Fatalf("error connecting to PostgreSQL: %v", err)
	}

	const createTable = `
CREATE TABLE IF NOT EXISTS transfers (
	id TEXT PRIMARY KEY,
	sender_id TEXT NOT NULL,
	receiver_id TEXT NOT NULL,
	currency TEXT NOT NULL,
	amount DOUBLE PRECISION NOT NULL,
	state TEXT NOT NULL
);`
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		logging.Logger.Fatalf("error creating PostgreSQL table: %v", err)
	}

	return &TransfersPostgresRepo{db: db}
}

func (r *TransfersPostgresRepo) Create(ctx context.Context, transfer models.Transfer) (string, error) {
	id := uuid.NewString()
	const query = `
INSERT INTO transfers (id, sender_id, receiver_id, currency, amount, state)
VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.db.ExecContext(ctx, query,
		id,
		transfer.SenderID,
		transfer.ReceiverID,
		transfer.Currency.String(),
		transfer.Amount,
		transfer.State,
	)
	if err != nil {
		return "", fmt.Errorf("error inserting transfer in PostgreSQL: %w", err)
	}
	return id, nil
}

func (r *TransfersPostgresRepo) GetByID(ctx context.Context, id string) (models.Transfer, error) {
	if _, err := uuid.Parse(id); err != nil {
		return models.Transfer{}, fmt.Errorf("error parsing transfer ID %s: %s: %w", id, err.Error(), known_errors.ErrBadRequest)
	}

	const query = `
SELECT id, sender_id, receiver_id, currency, amount, state
FROM transfers
WHERE id = $1`
	var transfer models.Transfer
	var currency string
	if err := r.db.QueryRowContext(ctx, query, id).Scan(
		&transfer.ID,
		&transfer.SenderID,
		&transfer.ReceiverID,
		&currency,
		&transfer.Amount,
		&transfer.State,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Transfer{}, fmt.Errorf("transfer not found: %w", known_errors.ErrNotFound)
		}
		return models.Transfer{}, fmt.Errorf("error getting transfer: %w", err)
	}

	transfer.Currency = enums.ParseCurrency(currency)
	return transfer, nil
}

func (r *TransfersPostgresRepo) Update(ctx context.Context, transfer models.Transfer) error {
	if _, err := uuid.Parse(transfer.ID); err != nil {
		return fmt.Errorf("error parsing transfer ID %s: %s: %w", transfer.ID, err.Error(), known_errors.ErrBadRequest)
	}

	setClauses := make([]string, 0, 5)
	args := make([]interface{}, 0, 6)
	argPos := 1

	if transfer.SenderID != "" {
		setClauses = append(setClauses, fmt.Sprintf("sender_id = $%d", argPos))
		args = append(args, transfer.SenderID)
		argPos++
	}
	if transfer.ReceiverID != "" {
		setClauses = append(setClauses, fmt.Sprintf("receiver_id = $%d", argPos))
		args = append(args, transfer.ReceiverID)
		argPos++
	}
	if transfer.Currency != enums.CurrencyUnknown {
		setClauses = append(setClauses, fmt.Sprintf("currency = $%d", argPos))
		args = append(args, transfer.Currency.String())
		argPos++
	}
	if transfer.Amount != 0 {
		setClauses = append(setClauses, fmt.Sprintf("amount = $%d", argPos))
		args = append(args, transfer.Amount)
		argPos++
	}
	if transfer.State != "" {
		setClauses = append(setClauses, fmt.Sprintf("state = $%d", argPos))
		args = append(args, transfer.State)
		argPos++
	}

	if len(setClauses) == 0 {
		return fmt.Errorf("no valid fields to update: %w", known_errors.ErrBadRequest)
	}

	query := fmt.Sprintf("UPDATE transfers SET %s WHERE id = $%d", strings.Join(setClauses, ", "), argPos)
	args = append(args, transfer.ID)

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("error updating transfer: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error checking updated rows: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("transfer not found: %w", known_errors.ErrNotFound)
	}
	return nil
}

func (r *TransfersPostgresRepo) Delete(ctx context.Context, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("error parsing transfer ID %s: %s: %w", id, err.Error(), known_errors.ErrBadRequest)
	}

	const query = `DELETE FROM transfers WHERE id = $1`
	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("error deleting transfer: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error checking deleted rows: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("transfer not found: %w", known_errors.ErrNotFound)
	}
	return nil
}

func (r *TransfersPostgresRepo) ListByUserID(ctx context.Context, userID string) ([]models.Transfer, error) {
	const query = `
	SELECT id, sender_id, receiver_id, currency, amount, state
	FROM transfers
	WHERE sender_id = $1 OR receiver_id = $1
	ORDER BY id`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("error listing transfers by user: %w", err)
	}
	defer rows.Close()

	var out []models.Transfer
	for rows.Next() {
		var t models.Transfer
		var currency string
		if err := rows.Scan(&t.ID, &t.SenderID, &t.ReceiverID, &currency, &t.Amount, &t.State); err != nil {
			return nil, fmt.Errorf("error scanning transfer row: %w", err)
		}
		t.Currency = enums.ParseCurrency(currency)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transfers: %w", err)
	}
	if out == nil {
		out = []models.Transfer{}
	}
	return out, nil
}
