package revolut

import (
	"context"
	"net/url"
	"time"
)

// Account is the Revolut Business account schema, distilled to the
// fields fsvc actually surfaces. The full payload has more
// (state-specific keys, fiat-vs-crypto markers, ...); we use
// json.Decoder which silently drops extras, so adding fields here
// later is non-breaking.
//
// Field naming is direct from Revolut's response, which is camelCase
// elsewhere but uses snake_case for these objects. The tags handle
// the mapping; Go-side code uses idiomatic Go field names.
type Account struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Balance   float64 `json:"balance"`
	Currency  string  `json:"currency"`
	State     string  `json:"state"`
	Public    bool    `json:"public"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// Card is the Revolut Business card schema, similarly trimmed.
type Card struct {
	ID        string `json:"id"`
	HolderID  string `json:"holder_id"`
	State     string `json:"state"`
	Label     string `json:"label"`
	LastFour  string `json:"last_four"`
	Brand     string `json:"brand"`
	AccountID string `json:"account_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Transaction is one row in the /transactions response. The amount /
// currency might be at the top level OR inside `legs[0]` depending on
// the transaction type (Revolut Business mixes the two shapes); we
// expose both so the caller can pick whichever is non-zero.
//
// The Python ListTransactions in revolut_list_transactions.py has the
// same fallback logic — see format_amount(). Replicating it here
// keeps Go output identical to the legacy script.
type Transaction struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	State       string `json:"state"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	CompletedAt string `json:"completed_at,omitempty"`
	Reference   string `json:"reference,omitempty"`
	// Top-level amount/currency (present on simple transactions).
	Amount   float64 `json:"amount,omitempty"`
	Currency string  `json:"currency,omitempty"`
	// Legs are present on transfers / FX / multi-account moves.
	Legs []TransactionLeg `json:"legs,omitempty"`
}

// TransactionLeg is one side of a multi-leg transaction. For a simple
// EUR → EUR move there's one leg; for FX there are two (one per
// currency).
type TransactionLeg struct {
	LegID        string  `json:"leg_id"`
	AccountID    string  `json:"account_id"`
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
	Description  string  `json:"description,omitempty"`
	BalanceAfter float64 `json:"balance,omitempty"`
}

// AmountAndCurrency returns the most-specific (amount, currency) pair
// available on this transaction, falling back to legs[0] if the
// top-level fields are zero. Mirrors Python's format_amount().
func (t *Transaction) AmountAndCurrency() (float64, string) {
	if t.Currency != "" {
		return t.Amount, t.Currency
	}
	if len(t.Legs) > 0 {
		return t.Legs[0].Amount, t.Legs[0].Currency
	}
	return 0, ""
}

// Description returns the most useful single-line description for
// this transaction, looking at the leg first then falling back to the
// top-level reference.
func (t *Transaction) Description() string {
	if len(t.Legs) > 0 && t.Legs[0].Description != "" {
		return t.Legs[0].Description
	}
	return t.Reference
}

// GetAccounts returns all accounts visible to the current API key.
// Revolut returns these as a flat array with no pagination — small
// enough that a single GET is fine.
func (c *Client) GetAccounts(ctx context.Context) ([]Account, error) {
	if err := c.EnsureAccessToken(ctx); err != nil {
		return nil, err
	}
	var out []Account
	if err := c.authedGet(ctx, "/accounts", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCards returns all cards visible to the current API key.
func (c *Client) GetCards(ctx context.Context) ([]Card, error) {
	if err := c.EnsureAccessToken(ctx); err != nil {
		return nil, err
	}
	var out []Card
	if err := c.authedGet(ctx, "/cards", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TransactionsOpts narrows a transactions query. AccountID is
// optional (nil = all accounts); From/To are RFC 3339; Count is
// per-page max (Revolut caps at 1000).
type TransactionsOpts struct {
	AccountID string
	From      time.Time
	To        time.Time
	Count     int
}

// GetTransactions queries /transactions with the given filters. If
// From is the zero value, defaults to 365 days ago. If To is zero,
// defaults to now. If Count is 0, defaults to 1000 (Revolut's max).
//
// Time formatting: RFC 3339 with explicit "Z" suffix — Revolut's API
// is picky about this and rejects naive "+00:00" form.
func (c *Client) GetTransactions(ctx context.Context, opts TransactionsOpts) ([]Transaction, error) {
	if err := c.EnsureAccessToken(ctx); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Truncate(time.Second)
	from := opts.From
	to := opts.To
	if to.IsZero() {
		to = now
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -365)
	}
	count := opts.Count
	if count <= 0 {
		count = 1000
	}

	params := url.Values{}
	params.Set("from", from.UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("to", to.UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("count", itoa(count))
	if opts.AccountID != "" {
		params.Set("account", opts.AccountID)
	}

	var out []Transaction
	if err := c.authedGet(ctx, "/transactions", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// itoa is a tiny strconv.Itoa shim to avoid pulling strconv in for one
// call. Inlined to keep this file dependency-light.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
