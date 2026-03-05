package http

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"backend-test-assignment/internal/withdrawal"
)

type Handler struct {
	service   *withdrawal.Service
	authToken string
	mux       *http.ServeMux
}

func NewHandler(service *withdrawal.Service, authToken string) http.Handler {
	h := &Handler{
		service:   service,
		authToken: authToken,
		mux:       http.NewServeMux(),
	}
	h.routes()
	return h.withMiddleware(h.mux)
}

func (h *Handler) routes() {
	h.mux.HandleFunc("POST /v1/withdrawals", h.handleCreateWithdrawal)
	h.mux.HandleFunc("GET /v1/withdrawals/{id}", h.handleGetWithdrawal)
	h.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func (h *Handler) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			log.Printf("method=%s path=%s duration=%s", r.Method, r.URL.Path, time.Since(start))
		}()

		if err := h.authorize(r); err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) authorize(r *http.Request) error {
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return errors.New("missing bearer token")
	}
	token := strings.TrimPrefix(authorization, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.authToken)) != 1 {
		return errors.New("invalid token")
	}
	return nil
}

func (h *Handler) handleCreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	req, err := decodeCreateWithdrawalRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	res, cached, err := h.service.CreateWithdrawal(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	status := http.StatusCreated
	if cached {
		status = res.HTTPStatus
	}

	writeJSON(w, status, res)
}

func (h *Handler) handleGetWithdrawal(w http.ResponseWriter, r *http.Request) {
	wd, err := h.service.GetWithdrawal(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, wd)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, withdrawal.ErrInvalidAmount),
		errors.Is(err, withdrawal.ErrInvalidCurrency),
		errors.Is(err, withdrawal.ErrInvalidDestination),
		errors.Is(err, withdrawal.ErrMissingIdempotencyKey),
		errors.Is(err, withdrawal.ErrInvalidUserID):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, withdrawal.ErrInsufficientFunds):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, withdrawal.ErrIdempotencyConflict):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, withdrawal.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func decodeCreateWithdrawalRequest(r *http.Request) (withdrawal.CreateRequest, error) {
	defer r.Body.Close()

	var req withdrawal.CreateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return withdrawal.CreateRequest{}, err
	}
	return req, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}