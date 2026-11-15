package rhx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// QuoteBudget is shared by all requests made through a QuoteConnection,
// including authentication. Limits must be positive and Deadline is absolute.
type QuoteBudget struct {
	Requests  int
	Bytes     int64
	Deadline  time.Time
	mu        sync.Mutex
	refreshed bool
}

var ErrQuoteBudget = errors.New("quote request budget exhausted")

type quoteTransport struct {
	base   http.RoundTripper
	budget *QuoteBudget
}

func (t *quoteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !allowedQuoteRequest(req) {
		return nil, errors.New("quote transport rejected endpoint")
	}
	t.budget.mu.Lock()
	if t.budget.Requests <= 0 || t.budget.Bytes <= 0 || !time.Now().Before(t.budget.Deadline) {
		t.budget.mu.Unlock()
		return nil, ErrQuoteBudget
	}
	t.budget.Requests--
	t.budget.mu.Unlock()
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &quoteBudgetBody{ReadCloser: resp.Body, budget: t.budget}
	return resp, nil
}

var quoteAuthPath = regexp.MustCompile(`^/(pathfinder/inquiries/[A-Za-z0-9-]+/user_view/|challenge/[A-Za-z0-9-]+/respond/|push/[A-Za-z0-9-]+/get_prompts_status/)$`)
var quoteForexPath = regexp.MustCompile(`^/marketdata/forex/quotes/[A-Za-z0-9-]+/$`)

func allowedQuoteRequest(r *http.Request) bool {
	if r.URL.Scheme != "https" || r.URL.User != nil {
		return false
	}
	path := r.URL.Path
	if r.URL.Host == "nummus.robinhood.com" {
		return r.Method == http.MethodGet && path == "/currency_pairs/"
	}
	if r.URL.Host != "api.robinhood.com" {
		return false
	}
	if r.Method == http.MethodGet && (path == "/quotes/" || quoteForexPath.MatchString(path)) {
		return true
	}
	if r.Method == http.MethodPost && (path == "/oauth2/token/" || path == "/pathfinder/user_machine/") {
		return true
	}
	return (r.Method == http.MethodGet || r.Method == http.MethodPost) && quoteAuthPath.MatchString(path)
}

type quoteBudgetBody struct {
	io.ReadCloser
	budget *QuoteBudget
}

func (b *quoteBudgetBody) Read(p []byte) (int, error) {
	b.budget.mu.Lock()
	defer b.budget.mu.Unlock()
	if int64(len(p)) > b.budget.Bytes+1 {
		p = p[:b.budget.Bytes+1]
	}
	n, err := b.ReadCloser.Read(p)
	if int64(n) > b.budget.Bytes {
		b.budget.Bytes = 0
		return n, ErrQuoteBudget
	}
	b.budget.Bytes -= int64(n)
	return n, err
}

// QuoteConnection uses only shared profile authentication and market-data reads.
// Legacy RHX clients that do not take the profile lock are not safe concurrent writers.
type QuoteConnection struct {
	portfolio *PortfolioConnection
	budget    *QuoteBudget
}

// NewQuoteConnection configures an injectable transport and a shared budget.
func NewQuoteConnection(profile, sessionDir string, transport http.RoundTripper, budget *QuoteBudget) (*QuoteConnection, error) {
	if budget == nil || budget.Requests <= 0 || budget.Bytes <= 0 || budget.Deadline.IsZero() {
		return nil, ErrQuoteBudget
	}
	p, err := NewPortfolioConnection(profile, sessionDir)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	p.auth.Client.client.Transport = &quoteTransport{base: transport, budget: budget}
	return &QuoteConnection{portfolio: p, budget: budget}, nil
}

func (c *QuoteConnection) context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ctx, c.budget.Deadline)
}

// QuoteCredentials are used only for explicit login and are never stored.
type QuoteCredentials struct{ Username, Password, MFACode string }

// Login explicitly authenticates; responses from authentication are never returned.
func (c *QuoteConnection) Login(ctx context.Context, credentials QuoteCredentials, waitForChallenge bool) error {
	ctx, cancel := c.context(ctx)
	defer cancel()
	ctx, loginCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer loginCancel()
	if strings.TrimSpace(credentials.Username) == "" || strings.TrimSpace(credentials.Password) == "" {
		return newError(ErrorValidation, "Credentials are required")
	}
	return c.portfolio.withSessionLock(ctx, func() error {
		a := c.portfolio.auth
		a.Client.clearSession()
		session, err := a.loginWithMFA(ctx, credentials.Username, credentials.Password, credentials.MFACode, waitForChallenge)
		if err != nil {
			return portfolioAuthError(err)
		}
		if err := saveSession(a.SessionPath, session); err != nil {
			return newError(ErrorAuthRequired, "Could not store brokerage session")
		}
		a.Client.setSession(session)
		return nil
	})
}

// QuoteResponse preserves a complete market-data response, including null and
// duplicate rows. Endpoint excludes query values and all sensitive headers.
type QuoteResponse struct {
	Endpoint    string
	Status      int
	RetryAfter  string
	RetrievedAt time.Time
	Body        []byte
}

var quoteSymbol = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,31}$`)
var quoteID = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)

// EquityQuotes returns the batch response without collapsing rows.
func (c *QuoteConnection) EquityQuotes(ctx context.Context, symbols []string) (QuoteResponse, error) {
	if len(symbols) == 0 {
		return QuoteResponse{}, newError(ErrorValidation, "Symbols are required")
	}
	for _, symbol := range symbols {
		if !quoteSymbol.MatchString(symbol) {
			return QuoteResponse{}, newError(ErrorValidation, "Invalid symbol")
		}
	}
	return c.read(ctx, robinhoodAPIBase, "/quotes/", url.Values{"symbols": {strings.Join(symbols, ",")}})
}

// CurrencyPairs returns the unmodified pair metadata response.
func (c *QuoteConnection) CurrencyPairs(ctx context.Context) (QuoteResponse, error) {
	return c.read(ctx, robinhoodCryptoBase, "/currency_pairs/", nil)
}

// ForexQuote retrieves one exact provider pair ID.
func (c *QuoteConnection) ForexQuote(ctx context.Context, id string) (QuoteResponse, error) {
	if !quoteID.MatchString(id) {
		return QuoteResponse{}, newError(ErrorValidation, "Invalid currency pair ID")
	}
	return c.read(ctx, robinhoodAPIBase, "/marketdata/forex/quotes/"+id+"/", nil)
}

func (c *QuoteConnection) read(ctx context.Context, base, path string, query url.Values) (result QuoteResponse, err error) {
	ctx, cancel := c.context(ctx)
	defer cancel()
	err = c.portfolio.withSessionLock(ctx, func() error {
		a := c.portfolio.auth
		session, e := loadSession(a.SessionPath)
		if e != nil || session.AccessToken == "" {
			return newError(ErrorAuthRequired, "Brokerage authentication required")
		}
		a.Client.setSession(session)
		result, e = c.get(ctx, base, path, query)
		if e != nil || (result.Status != 401 && result.Status != 403) {
			return e
		}
		if session.RefreshToken == "" {
			return newError(ErrorAuthRequired, "Brokerage authentication required")
		}
		c.budget.mu.Lock()
		alreadyRefreshed := c.budget.refreshed
		c.budget.refreshed = true
		c.budget.mu.Unlock()
		if alreadyRefreshed {
			return newError(ErrorAuthRequired, "Brokerage authentication required")
		}
		a.Client.clearSession()
		refreshed, e := a.refreshSession(ctx, session, false)
		if e != nil {
			return portfolioAuthError(e)
		}
		if e = saveSession(a.SessionPath, refreshed); e != nil {
			return newError(ErrorAuthRequired, "Could not store brokerage session")
		}
		a.Client.setSession(refreshed)
		result, e = c.get(ctx, base, path, query)
		return e
	})
	return result, err
}

func (c *QuoteConnection) get(ctx context.Context, base, path string, query url.Values) (QuoteResponse, error) {
	result := QuoteResponse{Endpoint: base + path}
	u := base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.portfolio.auth.Client.token)
	resp, err := c.portfolio.auth.Client.client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	result.Status, result.RetrievedAt = resp.StatusCode, time.Now().UTC()
	result.RetryAfter = safeRetryAfter(resp.Header.Get("Retry-After"))
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, err
	}
	result.Body = body
	return result, nil
}

func safeRetryAfter(value string) string {
	if len(value) > 128 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	if _, err := time.Parse(time.RFC1123, value); err == nil {
		return value
	}
	if value != "" {
		for _, ch := range value {
			if ch < '0' || ch > '9' {
				return ""
			}
		}
		return value
	}
	return ""
}

// Rows extracts raw JSON rows without collapsing duplicates or null entries.
func (r QuoteResponse) Rows() ([]json.RawMessage, error) {
	var payload struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(r.Body, &payload); err != nil {
		return nil, err
	}
	if payload.Results == nil {
		return nil, fmt.Errorf("market-data response has no results array")
	}
	return payload.Results, nil
}
