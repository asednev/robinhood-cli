package rhx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCashAccountBound(t *testing.T) {
	account := func(n string) map[string]any {
		return map[string]any{"account_number": n, "url": robinhoodAPIBase + "/accounts/" + n + "/", "brokerage_account_type": "individual", "state": "active", "management_type": "self_directed", "affiliate": "rhf", "type": "margin", "deactivated": false, "has_futures_account": false, "advisory_account_variant": nil}
	}
	for _, tc := range []struct {
		name, amount, currency, identity string
		status                           int
		malformed                        bool
		want                             bool
	}{
		{"positive", "123.4500", "USD", "001", 200, false, true},
		{"zero", "0", "USD", "001", 200, false, true},
		{"negative", "-42.13", "USD", "001", 200, false, true},
		{"wrong identity", "1", "USD", "002", 200, false, false},
		{"missing identity", "1", "USD", "", 200, false, false},
		{"wrong currency", "1", "EUR", "001", 200, false, false},
		{"missing currency", "1", "", "001", 200, false, false},
		{"missing amount", "", "USD", "001", 200, false, false},
		{"invalid amount", "1e2", "USD", "001", 200, false, false},
		{"malformed", "", "", "", 200, true, false},
		{"unauthorized", "", "", "", 401, false, false},
		{"rate limited", "", "", "", 429, false, false},
		{"server error", "", "", "", 500, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("unexpected method or authorization")
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/accounts/":
					json.NewEncoder(w).Encode(map[string]any{"results": []any{account("002"), account("001")}, "next": nil})
				case "/accounts/001/unified/":
					if tc.status != 200 {
						w.WriteHeader(tc.status)
						return
					}
					if tc.malformed {
						w.Write([]byte("{"))
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"account_number": tc.identity, "uninvested_cash": map[string]any{"amount": tc.amount, "currency_code": tc.currency}})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			if err := saveSession(sessionPath(dir, "default"), Session{AccessToken: "fixture"}); err != nil {
				t.Fatal(err)
			}
			c, err := NewPortfolioConnection("default", dir)
			if err != nil {
				t.Fatal(err)
			}
			target, _ := url.Parse(server.URL)
			c.auth.Client.client.Transport = quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Scheme != "https" || (r.URL.Host != "api.robinhood.com" && r.URL.Host != "bonfire.robinhood.com") || target.Hostname() != "127.0.0.1" {
					return nil, fmt.Errorf("unexpected fixture destination")
				}
				if (r.URL.Path == "/accounts/" && r.URL.Host != "api.robinhood.com") || (r.URL.Path == "/accounts/001/unified/" && r.URL.Host != "bonfire.robinhood.com") {
					return nil, fmt.Errorf("wrong host for path")
				}
				clone := r.Clone(r.Context())
				clone.URL.Scheme = target.Scheme
				clone.URL.Host = target.Host
				clone.Host = target.Host
				return http.DefaultTransport.RoundTrip(clone)
			})
			got, err := c.Cash(context.Background(), "001")
			if !tc.want {
				if err == nil || got != (CashObservation{}) {
					t.Fatal("expected unavailable observation")
				}
				wantCode := ErrorCashUnavailable
				switch tc.name {
				case "wrong identity":
					wantCode = ErrorBrokerAccountMismatch
				case "unauthorized":
					wantCode = ErrorAuthRequired
				case "rate limited":
					wantCode = ErrorRateLimited
				}
				var ce *CLIError
				if !errors.As(err, &ce) || ce.Code != wantCode {
					t.Fatalf("unexpected error: %v", err)
				}
				if strings.Contains(err.Error(), "001") {
					t.Fatal("account leaked")
				}
				return
			}
			if err != nil || got.Balance != tc.amount || got.Currency != "USD" || got.ObservedAt.IsZero() || got.ObservedAt.Location().String() != "UTC" {
				t.Fatalf("unexpected observation: %v", err)
			}
			if requests != 3 {
				t.Fatalf("unexpected request count: %d", requests)
			}
		})
	}
}

func TestCashMissingAccountAndTransport(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport bool
		want      ErrorCode
	}{
		{"missing account", false, ErrorBrokerAccountMismatch},
		{"transport failure", true, ErrorBrokerRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := saveSession(sessionPath(dir, "default"), Session{AccessToken: "fixture"}); err != nil {
				t.Fatal(err)
			}
			c, err := NewPortfolioConnection("default", dir)
			if err != nil {
				t.Fatal(err)
			}
			c.auth.Client.client.Transport = quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.robinhood.com" || r.URL.Path != "/accounts/" {
					t.Fatal("unexpected request")
				}
				if tc.transport {
					return nil, errors.New("fixture failure")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[],"next":null}`)), Request: r}, nil
			})
			got, err := c.Cash(context.Background(), "001")
			var ce *CLIError
			if !errors.As(err, &ce) || ce.Code != tc.want || got != (CashObservation{}) {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}

func TestCashRejectsAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{
		`{"account_number":"001","account_number":"002"}`,
		`{"uninvested_cash":{"amount":"1","amount":"2"}}`,
		`{"uninvested_cash":{"currency_code":"USD","currency_code":"EUR"}}`,
		`{"a":1} {"b":2}`,
	} {
		if uniqueJSON([]byte(raw)) {
			t.Fatal("accepted ambiguous JSON")
		}
	}
}

func TestCashUnifiedTransportFailure(t *testing.T) {
	c, err := NewPortfolioConnection("default", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.auth.Client.client.Transport = quoteRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Host != "bonfire.robinhood.com" || r.URL.Path != "/accounts/001/unified/" {
			t.Fatal("unexpected request")
		}
		return nil, errors.New("fixture transport failure")
	})
	got, err := c.cashLocked(context.Background(), "001")
	var ce *CLIError
	if !errors.As(err, &ce) || ce.Code != ErrorCashUnavailable || got != (CashObservation{}) {
		t.Fatal("expected unavailable cash")
	}
}
