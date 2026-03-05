package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"backend-test-assignment/internal/withdrawal"

	"github.com/lib/pq"
)

type WithdrawalRepository struct {
	db *sql.DB
}

func NewWithdrawalRepository(db *sql.DB) *WithdrawalRepository {
	return &WithdrawalRepository{db: db}
}

func (r *WithdrawalRepository) CreateWithdrawal(ctx context.Context, req withdrawal.CreateRequest, amount withdrawal.Amount) (withdrawal.CreateResponse, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return withdrawal.CreateResponse{}, false, err
	}
	defer tx.Rollback()

	payloadHash := req.PayloadHash(amount)
	idemID, cachedResponse, cached, err := r.lockOrCreateIdempotencyRecord(ctx, tx, req, payloadHash)
	if err != nil {
		return withdrawal.CreateResponse{}, false, err
	}
	if cached {
		return cachedResponse, true, tx.Commit()
	}

	balance, err := r.lockBalance(ctx, tx, req.UserID, req.Currency)
	if err != nil {
		return withdrawal.CreateResponse{}, false, err
	}
	if balance < amount.Micros {
		if err := r.storeIdempotencyFailure(ctx, tx, idemID, httpStatusConflict(), withdrawal.ErrInsufficientFunds.Error()); err != nil {
			return withdrawal.CreateResponse{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return withdrawal.CreateResponse{}, false, err
		}
		return withdrawal.CreateResponse{}, false, withdrawal.ErrInsufficientFunds
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE balances
		SET amount_micros = amount_micros - $1, updated_at = NOW()
		WHERE user_id = $2 AND currency = $3
	`, amount.Micros, req.UserID, req.Currency); err != nil {
		return withdrawal.CreateResponse{}, false, err
	}

	withdrawalID, createdAt, err := r.insertWithdrawal(ctx, tx, req, amount)
	if err != nil {
		return withdrawal.CreateResponse{}, false, err
	}

	response := withdrawal.NewCreateResponse(withdrawalID, req, amount, createdAt)
	body, err := json.Marshal(response)
	if err != nil {
		return withdrawal.CreateResponse{}, false, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET response_status = $1, response_body = $2, withdrawal_id = $3, updated_at = NOW()
		WHERE id = $4
	`, response.HTTPStatus, body, withdrawalID, idemID); err != nil {
		return withdrawal.CreateResponse{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return withdrawal.CreateResponse{}, false, err
	}
	return response, false, nil
}

func (r *WithdrawalRepository) GetWithdrawal(ctx context.Context, id string) (withdrawal.Withdrawal, error) {
	var (
		wd           withdrawal.Withdrawal
		amountMicros int64
		createdAt    time.Time
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, amount_micros, currency, destination, status, idempotency_key, created_at
		FROM withdrawals
		WHERE id = $1
	`, id).Scan(&wd.ID, &wd.UserID, &amountMicros, &wd.Currency, &wd.Destination, &wd.Status, &wd.IdempotencyKey, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return withdrawal.Withdrawal{}, withdrawal.ErrNotFound
	}
	if err != nil {
		return withdrawal.Withdrawal{}, err
	}
	wd.Amount = withdrawal.Amount{Micros: amountMicros}.String()
	wd.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	return wd, nil
}

func (r *WithdrawalRepository) lockOrCreateIdempotencyRecord(ctx context.Context, tx *sql.Tx, req withdrawal.CreateRequest, payloadHash string) (int64, withdrawal.CreateResponse, bool, error) {
	var row idempotencyRow
	err := tx.QueryRowContext(ctx, `
		SELECT id, payload_hash, response_status, response_body
		FROM idempotency_keys
		WHERE user_id = $1 AND idempotency_key = $2
		FOR UPDATE
	`, req.UserID, req.IdempotencyKey).Scan(&row.ID, &row.PayloadHash, &row.ResponseStatus, &row.ResponseBody)
	if err == nil {
		return row.resolve(payloadHash)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, withdrawal.CreateResponse{}, false, err
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO idempotency_keys (user_id, idempotency_key, payload_hash)
		VALUES ($1, $2, $3)
		RETURNING id
	`, req.UserID, req.IdempotencyKey, payloadHash).Scan(&row.ID)
	if err == nil {
		return row.ID, withdrawal.CreateResponse{}, false, nil
	}

	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		err = tx.QueryRowContext(ctx, `
			SELECT id, payload_hash, response_status, response_body
			FROM idempotency_keys
			WHERE user_id = $1 AND idempotency_key = $2
			FOR UPDATE
		`, req.UserID, req.IdempotencyKey).Scan(&row.ID, &row.PayloadHash, &row.ResponseStatus, &row.ResponseBody)
		if err != nil {
			return 0, withdrawal.CreateResponse{}, false, err
		}
		return row.resolve(payloadHash)
	}

	return 0, withdrawal.CreateResponse{}, false, err
}

func (r *WithdrawalRepository) lockBalance(ctx context.Context, tx *sql.Tx, userID int64, currency string) (int64, error) {
	var balance int64
	err := tx.QueryRowContext(ctx, `
		SELECT amount_micros
		FROM balances
		WHERE user_id = $1 AND currency = $2
		FOR UPDATE
	`, userID, currency).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, withdrawal.ErrInsufficientFunds
	}
	return balance, err
}

func (r *WithdrawalRepository) insertWithdrawal(ctx context.Context, tx *sql.Tx, req withdrawal.CreateRequest, amount withdrawal.Amount) (string, time.Time, error) {
	var id string
	var createdAt time.Time
	err := tx.QueryRowContext(ctx, `
		INSERT INTO withdrawals (user_id, amount_micros, currency, destination, status, idempotency_key)
		VALUES ($1, $2, $3, $4, 'pending', $5)
		RETURNING id, created_at
	`, req.UserID, amount.Micros, req.Currency, req.Destination, req.IdempotencyKey).Scan(&id, &createdAt)
	return id, createdAt, err
}

func (r *WithdrawalRepository) storeIdempotencyFailure(ctx context.Context, tx *sql.Tx, id int64, status int, message string) error {
	body, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET response_status = $1, response_body = $2, updated_at = NOW()
		WHERE id = $3
	`, status, body, id)
	return err
}

type idempotencyRow struct {
	ID             int64
	PayloadHash    string
	ResponseStatus sql.NullInt32
	ResponseBody   []byte
}

func (r idempotencyRow) resolve(expectedHash string) (int64, withdrawal.CreateResponse, bool, error) {
	if r.PayloadHash != "" && r.PayloadHash != expectedHash {
		return 0, withdrawal.CreateResponse{}, false, withdrawal.ErrIdempotencyConflict
	}
	if !r.ResponseStatus.Valid {
		return r.ID, withdrawal.CreateResponse{}, false, nil
	}

	if r.ResponseStatus.Int32 == httpStatusCreated() {
		var res withdrawal.CreateResponse
		if err := json.Unmarshal(r.ResponseBody, &res); err != nil {
			return 0, withdrawal.CreateResponse{}, false, err
		}
		res.HTTPStatus = int(r.ResponseStatus.Int32)
		return r.ID, res, true, nil
	}

	message := extractErrorMessage(r.ResponseBody)
	switch int(r.ResponseStatus.Int32) {
	case httpStatusConflict():
		return 0, withdrawal.CreateResponse{}, false, withdrawal.ErrInsufficientFunds
	case httpStatusUnprocessableEntity():
		return 0, withdrawal.CreateResponse{}, false, withdrawal.ErrIdempotencyConflict
	default:
		return 0, withdrawal.CreateResponse{}, false, fmt.Errorf("unexpected cached status %d: %s", r.ResponseStatus.Int32, message)
	}
}

func extractErrorMessage(body []byte) string {
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload["error"]
}

func httpStatusCreated() int32 {
	return 201
}

func httpStatusConflict() int {
	return 409
}

func httpStatusUnprocessableEntity() int {
	return 422
}
