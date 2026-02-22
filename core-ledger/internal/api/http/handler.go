// Package http provides a thin HTTP adapter over the LedgerService.
// It owns request parsing, response serialisation, and error mapping only.
// No business logic lives here.
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/gubaros/del-e-very/core-ledger/internal/application"
	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
)

// Handler wires HTTP requests to the application service.
type Handler struct {
	svc *application.LedgerService
}

// NewHandler constructs a Handler backed by svc.
func NewHandler(svc *application.LedgerService) *Handler {
	return &Handler{svc: svc}
}

// -------------------------------------------------------------------
// Request / Response DTOs
// -------------------------------------------------------------------

type postingDTO struct {
	AccountID   string `json:"account_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Narrative   string `json:"narrative,omitempty"`
	ExternalRef string `json:"external_ref,omitempty"`
}

type postTransactionRequest struct {
	TenantID       string       `json:"tenant_id"`
	IdempotencyKey string       `json:"idempotency_key"`
	TxType         string       `json:"tx_type"`
	ValueDate      time.Time    `json:"value_date"`
	Postings       []postingDTO `json:"postings"`
	ExternalRef    string       `json:"external_ref,omitempty"`
	Actor          string       `json:"actor,omitempty"`
	Channel        string       `json:"channel,omitempty"`
}

type reverseTransactionRequest struct {
	TenantID       string `json:"tenant_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Narrative      string `json:"narrative,omitempty"`
}

type createAccountRequest struct {
	TenantID    string `json:"tenant_id"`
	AccountID   string `json:"account_id"`
	Name        string `json:"name"`
	AccountType string `json:"account_type"`
	Currency    string `json:"currency"`
}

// -------------------------------------------------------------------
// Endpoint handlers
// -------------------------------------------------------------------

// PostTransaction handles POST /v1/transactions
func (h *Handler) PostTransaction(w http.ResponseWriter, r *http.Request) {
	correlationID := r.Header.Get("X-Correlation-Id")

	var req postTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.TenantID == "" || req.IdempotencyKey == "" || req.TxType == "" {
		writeError(w, http.StatusBadRequest, "tenant_id, idempotency_key, and tx_type are required")
		return
	}
	if len(req.Postings) < 2 {
		writeError(w, http.StatusBadRequest, "at least 2 postings are required")
		return
	}

	postings := make([]application.PostingInput, 0, len(req.Postings))
	for _, p := range req.Postings {
		postings = append(postings, application.PostingInput{
			LedgerAccountID: domain.AccountID(p.AccountID),
			AmountMinor:     p.AmountMinor,
			Currency:        domain.Currency(p.Currency),
			Narrative:       p.Narrative,
			ExternalRef:     p.ExternalRef,
		})
	}

	valueDate := req.ValueDate
	if valueDate.IsZero() {
		valueDate = time.Now().UTC()
	}

	cmd := application.PostTransactionCmd{
		TenantID:       domain.TenantID(req.TenantID),
		IdempotencyKey: domain.IdempotencyKey(req.IdempotencyKey),
		TxType:         domain.TxType(req.TxType),
		ValueDate:      valueDate,
		Postings:       postings,
		CorrelationID:  domain.CorrelationID(correlationID),
		ExternalRef:    req.ExternalRef,
		Actor:          req.Actor,
		Channel:        req.Channel,
	}

	tx, err := h.svc.Post(r.Context(), cmd)
	if err != nil {
		mapServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, tx)
}

// ReverseTransaction handles POST /v1/transactions/{tx_id}/reverse
func (h *Handler) ReverseTransaction(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "tx_id")
	correlationID := r.Header.Get("X-Correlation-Id")

	var req reverseTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.TenantID == "" || req.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "tenant_id and idempotency_key are required")
		return
	}

	cmd := application.ReverseTransactionCmd{
		TenantID:       domain.TenantID(req.TenantID),
		IdempotencyKey: domain.IdempotencyKey(req.IdempotencyKey),
		OriginalTxID:   domain.TxID(txID),
		Narrative:      req.Narrative,
		CorrelationID:  domain.CorrelationID(correlationID),
	}

	tx, err := h.svc.Reverse(r.Context(), cmd)
	if err != nil {
		mapServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, tx)
}

// GetByIdempotency handles GET /v1/transactions/by-idempotency?tenant=…&key=…
func (h *Handler) GetByIdempotency(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant")
	key := r.URL.Query().Get("key")

	if tenant == "" || key == "" {
		writeError(w, http.StatusBadRequest, "tenant and key query parameters are required")
		return
	}

	tx, err := h.svc.GetByIdempotency(r.Context(), domain.TenantID(tenant), domain.IdempotencyKey(key))
	if err != nil {
		mapServiceError(w, err)
		return
	}
	if tx == nil {
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	}

	writeJSON(w, http.StatusOK, tx)
}

// GetBalance handles GET /v1/balances/{account_id}?tenant=…
func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "account_id")
	tenant := r.URL.Query().Get("tenant")

	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant query parameter is required")
		return
	}

	balance, err := h.svc.GetBalance(r.Context(), domain.TenantID(tenant), domain.AccountID(accountID))
	if err != nil {
		mapServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tenant_id":     tenant,
		"account_id":    accountID,
		"balance_minor": balance.MinorUnits,
		"currency":      string(balance.Currency),
	})
}

// CreateAccount handles POST /v1/accounts
func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.TenantID == "" || req.AccountID == "" || req.Currency == "" || req.AccountType == "" {
		writeError(w, http.StatusBadRequest, "tenant_id, account_id, currency, and account_type are required")
		return
	}

	cmd := application.CreateAccountCmd{
		TenantID:    domain.TenantID(req.TenantID),
		AccountID:   domain.AccountID(req.AccountID),
		Name:        req.Name,
		AccountType: domain.AccountType(req.AccountType),
		Currency:    domain.Currency(req.Currency),
	}

	acct, err := h.svc.CreateAccount(r.Context(), cmd)
	if err != nil {
		mapServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, acct)
}

// -------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func mapServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrAccountNotFound):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrTransactionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInsufficientPostings),
		errors.Is(err, domain.ErrTenantMismatch),
		errors.Is(err, domain.ErrCurrencyMismatch),
		errors.Is(err, domain.ErrUnbalancedPostings):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
