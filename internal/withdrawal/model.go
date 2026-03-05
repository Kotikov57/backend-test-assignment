package withdrawal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const CurrencyUSDT = "USDT"

var (
	ErrInvalidUserID         = errors.New("user_id must be greater than 0")
	ErrInvalidAmount         = errors.New("amount must be greater than 0")
	ErrInvalidCurrency       = errors.New("currency must be USDT")
	ErrInvalidDestination    = errors.New("destination is required")
	ErrMissingIdempotencyKey = errors.New("idempotency_key is required")
	ErrInsufficientFunds     = errors.New("insufficient balance")
	ErrIdempotencyConflict   = errors.New("idempotency_key reuse with different payload")
	ErrNotFound              = errors.New("withdrawal not found")
)

type CreateRequest struct {
	UserID         int64       `json:"user_id"`
	Amount         json.Number `json:"amount"`
	Currency       string      `json:"currency"`
	Destination    string      `json:"destination"`
	IdempotencyKey string      `json:"idempotency_key"`
}

type Amount struct {
	Micros int64
}

func ParseAmount(value json.Number) (Amount, error) {
	raw := strings.TrimSpace(value.String())
	if raw == "" {
		return Amount{}, ErrInvalidAmount
	}

	if strings.HasPrefix(raw, "-") {
		return Amount{}, ErrInvalidAmount
	}

	parts := strings.Split(raw, ".")
	if len(parts) > 2 {
		return Amount{}, ErrInvalidAmount
	}

	whole := parts[0]
	if whole == "" {
		whole = "0"
	}

	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) > 6 {
			return Amount{}, fmt.Errorf("amount must have up to 6 decimal places")
		}
	}

	for len(fraction) < 6 {
		fraction += "0"
	}

	var wholePart int64
	for _, r := range whole {
		if r < '0' || r > '9' {
			return Amount{}, ErrInvalidAmount
		}
		wholePart = wholePart*10 + int64(r-'0')
	}

	var fractionPart int64
	for _, r := range fraction {
		if r < '0' || r > '9' {
			return Amount{}, ErrInvalidAmount
		}
		fractionPart = fractionPart*10 + int64(r-'0')
	}

	micros := wholePart*1_000_000 + fractionPart
	return Amount{Micros: micros}, nil
}

func (a Amount) String() string {
	whole := a.Micros / 1_000_000
	fraction := a.Micros % 1_000_000
	return fmt.Sprintf("%d.%06d", whole, fraction)
}

type CreateResponse struct {
	ID             string `json:"id"`
	UserID         int64  `json:"user_id"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	Destination    string `json:"destination"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key"`
	CreatedAt      string `json:"created_at"`
	HTTPStatus     int    `json:"-"`
}

type Withdrawal struct {
	ID             string `json:"id"`
	UserID         int64  `json:"user_id"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	Destination    string `json:"destination"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key"`
	CreatedAt      string `json:"created_at"`
}

func (r CreateRequest) Validate() (Amount, error) {
	if r.UserID <= 0 {
		return Amount{}, ErrInvalidUserID
	}
	amount, err := ParseAmount(r.Amount)
	if err != nil {
		return Amount{}, err
	}
	if r.Currency != CurrencyUSDT {
		return Amount{}, ErrInvalidCurrency
	}
	if strings.TrimSpace(r.Destination) == "" {
		return Amount{}, ErrInvalidDestination
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return Amount{}, ErrMissingIdempotencyKey
	}
	return amount, nil
}

func (r CreateRequest) PayloadHash(amount Amount) string {
	payload := fmt.Sprintf("%d|%s|%s|%s", r.UserID, amount.String(), r.Currency, r.Destination)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func NewCreateResponse(id string, req CreateRequest, amount Amount, createdAt time.Time) CreateResponse {
	return CreateResponse{
		ID:             id,
		UserID:         req.UserID,
		Amount:         amount.String(),
		Currency:       req.Currency,
		Destination:    req.Destination,
		Status:         "pending",
		IdempotencyKey: req.IdempotencyKey,
		CreatedAt:      createdAt.UTC().Format(time.RFC3339Nano),
		HTTPStatus:     201,
	}
}
