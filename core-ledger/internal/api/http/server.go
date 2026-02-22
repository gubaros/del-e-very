package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds and returns a chi router wired to the supplied Handler.
func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	r.Route("/v1", func(r chi.Router) {
		// Accounts
		r.Post("/accounts", h.CreateAccount)

		// Transactions
		r.Post("/transactions", h.PostTransaction)
		r.Post("/transactions/{tx_id}/reverse", h.ReverseTransaction)
		r.Get("/transactions/by-idempotency", h.GetByIdempotency)

		// Balances
		r.Get("/balances/{account_id}", h.GetBalance)
	})

	return r
}
