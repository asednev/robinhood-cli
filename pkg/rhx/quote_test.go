package rhx

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type quoteRoundTrip func(*http.Request) (*http.Response, error)

func (f quoteRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func quoteReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func testQuoteConnection(t *testing.T, transport http.RoundTripper, requests int, bytes int64) *QuoteConnection {
	t.Helper()
	dir := t.TempDir()
	if err := saveSession(sessionPath(dir, "default"), Session{AccessToken: "old", RefreshToken: "refresh", DeviceToken: "device"}); err != nil {
		t.Fatal(err)
	}
	c, err := NewQuoteConnection("default", dir, transport, &QuoteBudget{Requests: requests, Bytes: bytes, Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestQuoteRefreshAndRawRows(t *testing.T) {
	var paths []string
	c := testQuoteConnection(t, quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		switch len(paths) {
		case 1:
			if r.Header.Get("Authorization") != "Bearer old" {
				t.Fatal("missing old token")
			}
			return quoteReply(401, `{}`), nil
		case 2:
			if r.URL.Path != "/oauth2/token/" {
				t.Fatal(r.URL)
			}
			return quoteReply(200, `{"access_token":"new","refresh_token":"next"}`), nil
		default:
			if r.Header.Get("Authorization") != "Bearer new" {
				t.Fatal("missing refreshed token")
			}
			return quoteReply(200, `{"results":[{"symbol":"AAPL"},null,{"symbol":"AAPL"}]}`), nil
		}
	}), 3, 1000)
	r, err := c.EquityQuotes(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := r.Rows()
	if err != nil || len(rows) != 3 || string(rows[1]) != "null" {
		t.Fatalf("rows %s %v", rows, err)
	}
	if len(paths) != 3 || paths[0] != "/quotes/" || paths[2] != "/quotes/" {
		t.Fatal(paths)
	}
}

func TestQuoteBudgetsAndPaths(t *testing.T) {
	var paths []string
	c := testQuoteConnection(t, quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Host+r.URL.Path)
		return quoteReply(200, `{"results":[]}`), nil
	}), 2, 100)
	if _, err := c.CurrencyPairs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ForexQuote(context.Background(), "abc-123"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ForexQuote(context.Background(), "../bad"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	if _, err := c.EquityQuotes(context.Background(), []string{"AAPL"}); err == nil {
		t.Fatal("request limit ignored")
	}
	if strings.Join(paths, ",") != "nummus.robinhood.com/currency_pairs/,api.robinhood.com/marketdata/forex/quotes/abc-123/" {
		t.Fatal(paths)
	}
	c = testQuoteConnection(t, quoteRoundTrip(func(*http.Request) (*http.Response, error) { return quoteReply(200, "12345"), nil }), 1, 4)
	r, err := c.CurrencyPairs(context.Background())
	if err == nil || r.Body != nil {
		t.Fatalf("partial body exposed: %q %v", r.Body, err)
	}
}

func TestQuoteConcurrentRefreshUsesSharedLock(t *testing.T) {
	var mu sync.Mutex
	refreshes := 0
	transport := quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/oauth2/token/" {
			refreshes++
			return quoteReply(200, `{"access_token":"new"}`), nil
		}
		if r.Header.Get("Authorization") == "Bearer old" {
			return quoteReply(401, `{}`), nil
		}
		return quoteReply(200, `{"results":[]}`), nil
	})
	c := testQuoteConnection(t, transport, 4, 1000)
	other, err := NewQuoteConnection("default", filepath.Dir(c.portfolio.auth.SessionPath), transport, &QuoteBudget{Requests: 4, Bytes: 1000, Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, conn := range []*QuoteConnection{c, other} {
		wg.Add(1)
		go func(q *QuoteConnection) {
			defer wg.Done()
			if _, err := q.CurrencyPairsForSymbol(context.Background(), "BTC-USD"); err != nil {
				t.Error(err)
			}
		}(conn)
	}
	wg.Wait()
	if refreshes != 1 {
		t.Fatalf("refreshes = %d", refreshes)
	}
}

func TestQuoteLoginAndSingleReplay(t *testing.T) {
	requests := 0
	c := testQuoteConnection(t, quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/oauth2/token/" {
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if requests == 1 && (r.Form.Get("mfa_code") != "123456" || r.Form.Get("grant_type") != "password") {
				t.Fatal("explicit MFA not supplied")
			}
			return quoteReply(200, `{"access_token":"new","refresh_token":"next"}`), nil
		}
		return quoteReply(403, `{}`), nil
	}), 4, 1000)
	if err := c.Login(context.Background(), QuoteCredentials{Username: "user", Password: "secret", MFACode: "123456"}, false); err != nil {
		t.Fatal(err)
	}
	r, err := c.CurrencyPairs(context.Background())
	if err != nil || r.Status != 403 || requests != 4 {
		t.Fatalf("status=%d requests=%d err=%v", r.Status, requests, err)
	}
}

func TestQuoteRefreshChallengeStopsWithoutPassword(t *testing.T) {
	requests := 0
	c := testQuoteConnection(t, quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/oauth2/token/" {
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("password") != "" {
				t.Fatal("unexpected login")
			}
			return quoteReply(400, `{"verification_workflow":{"id":"challenge"}}`), nil
		}
		return quoteReply(401, `{}`), nil
	}), 5, 1000)
	if _, err := c.CurrencyPairs(context.Background()); err == nil || requests != 2 {
		t.Fatalf("requests=%d err=%v", requests, err)
	}
}

func TestQuoteMetadataAndExactBodyBudget(t *testing.T) {
	body := `{"results":[]}`
	c := testQuoteConnection(t, quoteRoundTrip(func(*http.Request) (*http.Response, error) {
		r := quoteReply(429, body)
		r.Header.Set("Retry-After", "30")
		return r, nil
	}), 1, int64(len(body)))
	r, err := c.CurrencyPairs(context.Background())
	if err != nil || string(r.Body) != body || r.Status != 429 || r.RetryAfter != "30" || r.RetrievedAt.IsZero() || r.Endpoint != "https://nummus.robinhood.com/currency_pairs/" {
		t.Fatalf("response=%+v err=%v", r, err)
	}
}

func TestQuoteRefreshWriteFailurePreservesSessionAndStops(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions")
	if err := saveSession(sessionPath(dir, "default"), Session{AccessToken: "old", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	transport := quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return quoteReply(401, `{}`), nil
		}
		if err := os.Rename(dir, dir+"-original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dir+"-original", dir); err != nil {
			t.Fatal(err)
		}
		return quoteReply(200, `{"access_token":"new"}`), nil
	})
	c, err := NewQuoteConnection("default", dir, transport, &QuoteBudget{Requests: 3, Bytes: 1000, Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CurrencyPairs(context.Background()); err == nil || requests != 2 {
		t.Fatalf("requests=%d err=%v", requests, err)
	}
	s, err := loadSession(sessionPath(dir+"-original", "default"))
	if err != nil || s.AccessToken != "old" || s.RefreshToken != "refresh" {
		t.Fatalf("old session not preserved: %+v %v", s, err)
	}
}

func TestCurrencyPairsForSymbolPreservesResponse(t *testing.T) {
	const body = " {\n\"next\":\"https://example.invalid/?secret=x\",\"results\":[{\"symbol\":\"OTHER\"},null,{\"symbol\":\"OTHER\"}]} "
	calls := 0
	c := testQuoteConnection(t, quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.String() != "https://nummus.robinhood.com/currency_pairs/?symbol=BTC-USD" || r.Header.Get("Authorization") != "Bearer old" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Redacted())
		}
		return quoteReply(200, body), nil
	}), 1, 1000)
	r, err := c.CurrencyPairsForSymbol(context.Background(), "BTC-USD")
	if err != nil || string(r.Body) != body || r.Endpoint != "https://nummus.robinhood.com/currency_pairs/" || r.RetrievedAt.IsZero() || calls != 1 {
		t.Fatalf("response=%+v calls=%d err=%v", r, calls, err)
	}
	rows, err := r.Rows()
	if err != nil || len(rows) != 3 || string(rows[0]) != string(rows[2]) || string(rows[1]) != "null" {
		t.Fatalf("rows=%s err=%v", rows, err)
	}
}

func TestCurrencyPairsForSymbolValidationAndEmpty(t *testing.T) {
	calls := 0
	c := testQuoteConnection(t, quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.RawQuery != "symbol=A.B-1" {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		return quoteReply(200, `{"results":[],"next":null}`), nil
	}), 1, 1000)
	for _, symbol := range []string{"", " BTC-USD", "BTC-USD\n", "BTC&symbol=ETH", "BTC/USD", "BTC%2DUSD", strings.Repeat("A", 33)} {
		if _, err := c.CurrencyPairsForSymbol(context.Background(), symbol); err == nil {
			t.Errorf("accepted %q", symbol)
		}
	}
	r, err := c.CurrencyPairsForSymbol(context.Background(), "A.B-1")
	rows, rowErr := r.Rows()
	if err != nil || rowErr != nil || len(rows) != 0 || calls != 1 {
		t.Fatalf("calls=%d rows=%s err=%v rowErr=%v", calls, rows, err, rowErr)
	}
}

func TestCurrencyPairsForSymbolRefreshAndLimits(t *testing.T) {
	calls := 0
	c := testQuoteConnection(t, quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/oauth2/token/" {
			return quoteReply(200, `{"access_token":"new","refresh_token":"next"}`), nil
		}
		if r.URL.RawQuery != "symbol=BTC-USD" {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		return quoteReply(401, `{"private":"rejected"}`), nil
	}), 5, 1000)
	r, err := c.CurrencyPairsForSymbol(context.Background(), "BTC-USD")
	if err != nil || r.Status != 401 || calls != 3 {
		t.Fatalf("status=%d calls=%d err=%v", r.Status, calls, err)
	}
	if _, err = c.CurrencyPairsForSymbol(context.Background(), "BTC-USD"); err == nil || calls != 4 {
		t.Fatalf("second refresh: calls=%d err=%v", calls, err)
	}

	c = testQuoteConnection(t, quoteRoundTrip(func(*http.Request) (*http.Response, error) { return quoteReply(200, "12345"), nil }), 1, 4)
	r, err = c.CurrencyPairsForSymbol(context.Background(), "BTC-USD")
	if err == nil || r.Body != nil {
		t.Fatalf("partial body=%q err=%v", r.Body, err)
	}
	if _, err = c.CurrencyPairsForSymbol(context.Background(), "BTC-USD"); err == nil {
		t.Fatal("request budget ignored")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.CurrencyPairsForSymbol(ctx, "BTC-USD"); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestCurrencyPairsForSymbolMetadataAndRedirect(t *testing.T) {
	c := testQuoteConnection(t, quoteRoundTrip(func(*http.Request) (*http.Response, error) {
		r := quoteReply(429, `{"results":[],"next":"opaque"}`)
		r.Header.Set("Retry-After", "30")
		return r, nil
	}), 1, 1000)
	r, err := c.CurrencyPairsForSymbol(context.Background(), "BTC-USD")
	if err != nil || r.Status != 429 || r.RetryAfter != "30" || string(r.Body) != `{"results":[],"next":"opaque"}` || strings.Contains(r.Endpoint, "BTC") {
		t.Fatalf("response=%+v err=%v", r, err)
	}

	calls := 0
	c = testQuoteConnection(t, quoteRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		r := quoteReply(http.StatusFound, "")
		r.Header.Set("Location", "https://example.invalid/steal")
		return r, nil
	}), 2, 1000)
	r, err = c.CurrencyPairsForSymbol(context.Background(), "BTC-USD")
	if err != nil || r.Status != http.StatusFound || calls != 1 {
		t.Fatalf("redirect: status=%d calls=%d err=%v", r.Status, calls, err)
	}
}
