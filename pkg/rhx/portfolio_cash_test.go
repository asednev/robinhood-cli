package rhx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCashAccountBound(t *testing.T) {
	for _, state := range []struct {
		name, accountType string
		fields            map[string]any
	}{
		{"cash unknown state", "cash", nil},
		{"margin unknown state", "margin", nil},
		{"margin unsettled credit", "margin", map[string]any{"margin_balances": map[string]any{"unsettled_funds": "25.00"}}},
		{"margin unsettled debit", "margin", map[string]any{"margin_balances": map[string]any{"unsettled_debit": "25.00"}}},
		{"sweep enabled", "margin", map[string]any{"sweep_enabled": true, "sweep_enrolled": true}},
		{"sweep disabled", "cash", map[string]any{"sweep_enabled": false, "sweep_enrolled": false, "unsettled_funds": "0", "unsettled_debit": "0"}},
	} {
		account := func(n string) map[string]any {
			row := map[string]any{"account_number": n, "url": robinhoodAPIBase + "/accounts/" + n + "/", "brokerage_account_type": "individual", "state": "active", "management_type": "self_directed", "affiliate": "rhf", "type": state.accountType, "deactivated": false, "has_futures_account": false, "advisory_account_variant": nil}
			for key, value := range state.fields {
				row[key] = value
			}
			return row
		}
		for _, tc := range []struct {
			name, amount, currency, identity string
			status                           int
			malformed                        bool
		}{
			{"positive USD", "123.4500", "USD", "001", 200, false},
			{"zero USD", "0", "USD", "001", 200, false},
			{"negative USD", "-42.13", "USD", "001", 200, false},
			{"precise USD", "9007199254740993.123456789012345678", "USD", "001", 200, false},
			{"wrong identity", "1", "USD", "002", 200, false},
			{"missing identity", "1", "USD", "", 200, false},
			{"wrong currency", "1", "EUR", "001", 200, false},
			{"missing currency", "1", "", "001", 200, false},
			{"missing amount", "", "USD", "001", 200, false},
			{"invalid amount", "1e2", "USD", "001", 200, false},
			{"double sign", "--1", "USD", "001", 200, false},
			{"plus sign", "+1", "USD", "001", 200, false},
			{"excess precision", "1.1234567890123456789", "USD", "001", 200, false},
			{"malformed", "", "", "", 200, true},
			{"unauthorized", "", "", "", 401, false},
			{"forbidden", "", "", "", 403, false},
			{"rate limited", "", "", "", 429, false},
			{"server error", "", "", "", 500, false},
		} {
			t.Run(state.name+"/"+tc.name, func(t *testing.T) {
				requests := 0
				cashReads := 0
				const nextAmount = "-9007199254740993.123456789012345678"
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
						cashReads++
						if tc.status != 200 {
							w.WriteHeader(tc.status)
							return
						}
						if tc.malformed {
							w.Write([]byte("{"))
							return
						}
						amount := tc.amount
						if cashReads > 1 {
							amount = nextAmount
						}
						json.NewEncoder(w).Encode(map[string]any{"account_number": tc.identity, "uninvested_cash": map[string]any{"amount": amount, "currency_code": tc.currency}, "account_buying_power": "999", "equity": "888", "cash_available_for_withdrawal": "777"})
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
				sessionBefore, err := os.ReadFile(sessionPath(dir, "default"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					after, err := os.ReadFile(sessionPath(dir, "default"))
					if err != nil || !bytes.Equal(sessionBefore, after) {
						t.Error("cash read modified session")
					}
					entries, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range entries {
						name := filepath.Base(sessionPath(dir, "default"))
						if entry.Name() != name && entry.Name() != name+".lock" {
							t.Errorf("cash read created unexpected file %s", entry.Name())
						}
					}
				})
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
				started := time.Now()
				got, err := c.Cash(context.Background(), "001")
				if requests != 3 {
					t.Fatalf("unexpected request count: %d", requests)
				}
				switch tc.name {
				case "positive USD", "zero USD", "negative USD", "precise USD":
					if err != nil || got.Balance != tc.amount || got.Currency != "USD" || got.ObservedAt.Before(started) || got.ObservedAt.After(time.Now()) || got.ObservedAt.Location() != time.UTC {
						t.Fatalf("expected exact fresh USD cash observation: %v", err)
					}
					started = time.Now()
					again, err := c.Cash(context.Background(), "001")
					if err != nil || again.Balance != nextAmount || again.Currency != "USD" || again.ObservedAt.Before(started) || again.ObservedAt.After(time.Now()) || !again.ObservedAt.After(got.ObservedAt) || again.ObservedAt.Location() != time.UTC || requests != 6 || cashReads != 2 {
						t.Fatalf("second call did not fetch a fresh cash observation: %v", err)
					}
					return
				}
				if err == nil || got != (CashObservation{}) {
					t.Fatal("expected unavailable observation")
				}
				wantCode := ErrorCashUnavailable
				switch tc.name {
				case "wrong identity":
					wantCode = ErrorBrokerAccountMismatch
				case "unauthorized", "forbidden":
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
			})
		}
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
