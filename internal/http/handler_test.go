package http_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	apihttp "backend-test-assignment/internal/http"
	"backend-test-assignment/internal/store"
	"backend-test-assignment/internal/withdrawal"

	_ "github.com/lib/pq"
)

const (
	dbOpTimeout      = 5 * time.Second
	requestTimeout   = 5 * time.Second
	concurrentWindow = 8 * time.Second
)

func TestCreateWithdrawalSuccess(t *testing.T) {
	h, db := newTestHandler(t)
	resetDB(t, db, 20_000_000)

	body := `{"user_id":1,"amount":"10.000000","currency":"USDT","destination":"wallet-a","idempotency_key":"k1"}`
	rec := performRequest(t, h, http.MethodPost, "/v1/withdrawals", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateWithdrawalInsufficientFunds(t *testing.T) {
	h, db := newTestHandler(t)
	resetDB(t, db, 5_000_000)

	body := `{"user_id":1,"amount":"10.000000","currency":"USDT","destination":"wallet-a","idempotency_key":"k2"}`
	rec := performRequest(t, h, http.MethodPost, "/v1/withdrawals", body)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateWithdrawalIdempotency(t *testing.T) {
	h, db := newTestHandler(t)
	resetDB(t, db, 20_000_000)

	body := `{"user_id":1,"amount":"10.000000","currency":"USDT","destination":"wallet-a","idempotency_key":"same-key"}`
	first := performRequest(t, h, http.MethodPost, "/v1/withdrawals", body)
	second := performRequest(t, h, http.MethodPost, "/v1/withdrawals", body)

	if first.Code != http.StatusCreated {
		t.Fatalf("expected first request to return 201, got %d", first.Code)
	}
	if second.Code != http.StatusCreated {
		t.Fatalf("expected second request to return cached 201, got %d", second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("expected identical response bodies, got %q vs %q", first.Body.String(), second.Body.String())
	}
}

func TestCreateWithdrawalConcurrent(t *testing.T) {
	h, db := newTestHandler(t)
	resetDB(t, db, 15_000_000)

	bodyA := `{"user_id":1,"amount":"10.000000","currency":"USDT","destination":"wallet-a","idempotency_key":"c1"}`
	bodyB := `{"user_id":1,"amount":"10.000000","currency":"USDT","destination":"wallet-b","idempotency_key":"c2"}`

	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, body := range []string{bodyA, bodyB} {
		wg.Add(1)
		go func(payload string) {
			defer wg.Done()
			rec := performRequest(t, h, http.MethodPost, "/v1/withdrawals", payload)
			codes <- rec.Code
		}(body)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(concurrentWindow):
		t.Fatal("concurrent test timed out waiting for requests")
	}

	close(codes)

	var created, conflicts int
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		}
	}

	if created != 1 || conflicts != 1 {
		t.Fatalf("expected one 201 and one 409, got created=%d conflicts=%d", created, conflicts)
	}
}

func newTestHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), dbOpTimeout)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	applyMigration(t, db)
	repository := store.NewWithdrawalRepository(db)
	service := withdrawal.NewService(repository)
	return apihttp.NewHandler(service, "test-token"), db
}

func applyMigration(t *testing.T, db *sql.DB) {
	t.Helper()

	content, err := os.ReadFile("../../migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbOpTimeout)
	defer cancel()
	if _, err := db.ExecContext(ctx, string(content)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
}

func resetDB(t *testing.T, db *sql.DB, balance int64) {
	t.Helper()

	queries := []string{
		"DELETE FROM idempotency_keys",
		"DELETE FROM withdrawals",
		"DELETE FROM balances",
		fmt.Sprintf("INSERT INTO balances (user_id, currency, amount_micros) VALUES (1, 'USDT', %d)", balance),
	}

	for _, q := range queries {
		ctx, cancel := context.WithTimeout(context.Background(), dbOpTimeout)
		_, err := db.ExecContext(ctx, q)
		cancel()
		if err != nil {
			t.Fatalf("reset db with %q: %v", q, err)
		}
	}
}

func performRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	reqCtx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	t.Cleanup(cancel)

	req := httptest.NewRequest(method, path, bytes.NewBufferString(body)).WithContext(reqCtx)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
