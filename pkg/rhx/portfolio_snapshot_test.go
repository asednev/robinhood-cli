package rhx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPortfolioSnapshotSynthetic(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(string, map[string]any, int)
		wantErr  bool
		wantLots int
	}{
		{name: "two lots with reserved shares", wantLots: 2},
		{name: "ETF lots", wantLots: 2, mutate: func(p string, m map[string]any, _ int) {
			if p == "/instruments/abc/" {
				m["type"] = "etp"
				m["tax_security_type"] = "etf"
			}
		}},
		{name: "ETN rejected", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/instruments/abc/" {
				m["type"] = "etp"
				m["tax_security_type"] = "etn"
			}
		}},
		{name: "missing ETF subtype", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/instruments/abc/" {
				m["type"] = "etp"
			}
		}},
		{name: "unknown ETF subtype", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/instruments/abc/" {
				m["type"] = "etp"
				m["tax_security_type"] = "other"
			}
		}},
		{name: "matching optional references", wantLots: 2, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				row := m["results"].([]any)[0].(map[string]any)
				row["account"] = "https://api.robinhood.com/accounts/001/"
				row["instrument"] = "https://api.robinhood.com/instruments/abc/"
			}
		}},
		{name: "missing direct account", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				delete(m["results"].([]any)[0].(map[string]any), "account_number")
			}
		}},
		{name: "missing direct instrument", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				delete(m["results"].([]any)[0].(map[string]any), "instrument_id")
			}
		}},
		{name: "foreign direct instrument", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["instrument_id"] = "other"
			}
		}},
		{name: "contradictory account reference", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["account"] = "https://api.robinhood.com/accounts/002/"
			}
		}},
		{name: "contradictory instrument reference", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["instrument"] = "https://api.robinhood.com/instruments/other/"
			}
		}},
		{name: "null account reference", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["account"] = nil
			}
		}},
		{name: "duplicate lot ID", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				rows := m["results"].([]any)
				rows[1].(map[string]any)["open_lot_id"] = "one"
			}
		}},
		{name: "foreign owner", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["account_number"] = "002"
			}
		}},
		{name: "mismatched lot sum", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["quantity"] = "2"
			}
		}},
		{name: "excess basis precision ignored", wantLots: 2, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["tax_cost_basis"] = "1.0000000000000000001"
			}
		}},
		{name: "excess quantity precision", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["quantity"] = "1.2500000000000000001"
			}
		}},
		{name: "malformed basis ignored", wantLots: 2, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["tax_cost_basis"] = map[string]any{"unexpected": true}
			}
		}},
		{name: "explicit foreign currency", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["basis_currency"] = "EUR"
			}
		}},
		{name: "explicit USD", wantLots: 2, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[1].(map[string]any)["basis_currency"] = "USD"
			}
		}},
		{name: "malformed date", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["open_date"] = "2025-02-30"
			}
		}},
		{name: "unknown date", wantLots: 2, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[1].(map[string]any)["open_date"] = nil
			}
		}},
		{name: "missing basis ignored", wantLots: 2, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				delete(m["results"].([]any)[0].(map[string]any), "tax_cost_basis")
			}
		}},
		{name: "invalid available quantity", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if strings.HasPrefix(p, "/tax_lots/") {
				m["results"].([]any)[0].(map[string]any)["quantity_available"] = "2"
			}
		}},
		{name: "inventory changed", wantErr: true, mutate: func(p string, m map[string]any, n int) {
			if p == "/positions/" && n == 2 {
				m["results"].([]any)[0].(map[string]any)["quantity"] = "9"
			}
		}},
		{name: "unsupported option", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/options/positions/" {
				m["results"] = []any{map[string]any{"account_number": "001", "account": "https://api.robinhood.com/accounts/001/", "quantity": "1"}}
			}
		}},
		{name: "unsupported type", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/instruments/abc/" {
				m["type"] = "warrant"
			}
		}},
		{name: "missing page field", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/accounts/" {
				delete(m, "next")
			}
		}},
		{name: "foreign next", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/accounts/" {
				m["next"] = "https://attacker.example/accounts/?default_to_all_accounts=true"
			}
		}},
		{name: "repeated next", wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/accounts/" {
				m["next"] = "https://api.robinhood.com/accounts/?default_to_all_accounts=true"
			}
		}},
	}
	for _, field := range []struct {
		key   string
		wrong any
	}{
		{"management_type", "managed"}, {"affiliate", "other"}, {"type", "ira"},
		{"deactivated", true}, {"has_futures_account", true}, {"advisory_account_variant", "managed"},
	} {
		for _, missing := range []bool{false, true} {
			name := "invalid " + field.key
			if missing {
				name = "missing " + field.key
			}
			cases = append(cases, struct {
				name     string
				mutate   func(string, map[string]any, int)
				wantErr  bool
				wantLots int
			}{name: name, wantErr: true, mutate: func(p string, m map[string]any, _ int) {
				if p == "/accounts/" {
					row := m["results"].([]any)[1].(map[string]any)
					if missing {
						delete(row, field.key)
					} else {
						row[field.key] = field.wrong
					}
				}
			}})
		}
	}
	for _, key := range []string{"management_type", "affiliate", "type", "deactivated", "has_futures_account", "advisory_account_variant"} {
		cases = append(cases, struct {
			name     string
			mutate   func(string, map[string]any, int)
			wantErr  bool
			wantLots int
		}{name: "wrong type " + key, wantErr: true, mutate: func(p string, m map[string]any, _ int) {
			if p == "/accounts/" {
				m["results"].([]any)[1].(map[string]any)[key] = []any{}
			}
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			positions := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("missing fixture token")
				}
				page := func(rows ...any) map[string]any {
					if rows == nil {
						rows = []any{}
					}
					return map[string]any{"results": rows, "next": nil}
				}
				account := func(n string) map[string]any {
					return map[string]any{"account_number": n, "url": "https://api.robinhood.com/accounts/" + n + "/", "brokerage_account_type": "individual", "state": "active", "management_type": "self_directed", "affiliate": "rhf", "type": "margin", "deactivated": false, "has_futures_account": false, "advisory_account_variant": nil}
				}
				owner := func() map[string]any {
					return map[string]any{"account_number": "001", "account": "https://api.robinhood.com/accounts/001/", "instrument": "https://api.robinhood.com/instruments/abc/"}
				}
				var m map[string]any
				switch r.URL.Path {
				case "/accounts/":
					m = page(account("002"), account("001"))
				case "/positions/":
					if r.URL.Query().Get("account_number") == "" {
						m = page()
						break
					}
					positions++
					x := owner()
					x["quantity"] = "1.5"
					m = page(x)
				case "/options/positions/":
					m = page()
				case "/portfolios/001/":
					m = map[string]any{"account": "https://api.robinhood.com/accounts/001/", "market_value": "100"}
				case "/instruments/abc/":
					m = map[string]any{"id": "abc", "symbol": "XYZ", "type": "stock"}
				case "/tax_lots/open/001/abc/":
					a, b := owner(), owner()
					for _, row := range []map[string]any{a, b} {
						delete(row, "account")
						delete(row, "instrument")
						row["instrument_id"] = "abc"
					}
					a["open_lot_id"] = "one"
					a["quantity"] = "1.25"
					a["quantity_available"] = "0.25"
					a["tax_cost_basis"] = "51.123"
					a["open_date"] = "2025-01-02"
					b["open_lot_id"] = "two"
					b["quantity"] = "0.25"
					b["tax_cost_basis"] = nil
					b["open_date"] = "2024-06-07"
					m = page(a, b)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				if tc.mutate != nil {
					tc.mutate(r.URL.Path, m, positions)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(m)
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
			c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
			snapshot, err := c.Snapshot(context.Background(), "001")
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if strings.Contains(err.Error(), "001") {
					t.Fatalf("account leaked: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Lots) != tc.wantLots || snapshot.WriteEligible {
				t.Fatalf("unexpected snapshot: %+v", snapshot)
			}
			if snapshot.Lots[0].Quantity != "1.25" || snapshot.Lots[0].OpenDate == nil || *snapshot.Lots[0].OpenDate != "2025-01-02" || snapshot.Lots[0].TaxCostBasis != nil || snapshot.Lots[0].BasisCurrency != nil || snapshot.Lots[1].Quantity != "0.25" || snapshot.Lots[1].TaxCostBasis != nil {
				t.Fatal("lot mapping failed")
			}
			if tc.name == "unknown date" {
				if snapshot.Lots[1].OpenDate != nil {
					t.Fatal("unknown date not retained")
				}
			} else if snapshot.Lots[1].OpenDate == nil || *snapshot.Lots[1].OpenDate != "2024-06-07" {
				t.Fatal("individual date not retained")
			}
			if tc.name == "explicit USD" && (snapshot.Lots[1].BasisCurrency == nil || *snapshot.Lots[1].BasisCurrency != "USD") {
				t.Fatal("USD currency not retained")
			}
		})
	}
}

func TestPortfolioPaginationDoesNotSendToForeignHost(t *testing.T) {
	var leaked bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[],"next":%q}`, foreign.URL+"/accounts/")
	}))
	defer server.Close()
	c, err := NewPortfolioConnection("default", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(server.URL)
	c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
	c.auth.Client.token = "Bearer fixture"
	if _, err := c.portfolioPages(context.Background(), "/accounts/", nil); err == nil {
		t.Fatal("expected error")
	}
	if leaked {
		t.Fatal("sent request to foreign host")
	}
}

func TestPortfolioPagesAndRedirects(t *testing.T) {
	var foreignRequests int
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignRequests++ }))
	defer foreign.Close()
	for _, mode := range []string{"pages", "redirect", "malformed", "http_error"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "pages":
					if r.URL.Query().Get("cursor") == "" {
						fmt.Fprint(w, `{"results":[{"id":"one"}],"next":"https://api.robinhood.com/positions/?account_number=001&cursor=next"}`)
					} else {
						fmt.Fprint(w, `{"results":[{"id":"two"}],"next":null}`)
					}
				case "redirect":
					http.Redirect(w, r, foreign.URL, http.StatusFound)
				case "malformed":
					fmt.Fprint(w, `{"results":{}}`)
				default:
					w.WriteHeader(503)
					fmt.Fprint(w, `{"detail":"secret broker payload"}`)
				}
			}))
			defer server.Close()
			c, err := NewPortfolioConnection("default", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			target, _ := url.Parse(server.URL)
			c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
			c.auth.Client.token = "Bearer private"
			rows, err := c.portfolioPages(context.Background(), "/positions/", url.Values{"account_number": {"001"}})
			if mode == "pages" {
				if err != nil || len(rows) != 2 {
					t.Fatalf("pages: %v, %v", rows, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "001") {
				t.Fatalf("unsafe result: %v", err)
			}
		})
	}
	if foreignRequests != 0 {
		t.Fatalf("redirect forwarded to foreign host %d times", foreignRequests)
	}
}

func TestPortfolioEmptyInventoryValuation(t *testing.T) {
	// A separate zero-unit crypto record is outside the scoped brokerage API.
	// The reader must neither include it nor make crypto calls.
	for _, market := range []string{"0.00", "7.00"} {
		t.Run(market, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/holdings/":
					t.Error("unrelated crypto account was queried")
					fmt.Fprint(w, `{"results":[{"quantity":"0","account_number":"crypto-other"}],"next":null}`)
				case "/accounts/":
					fmt.Fprint(w, `{"results":[{"account_number":"001","url":"https://api.robinhood.com/accounts/001/","brokerage_account_type":"individual","state":"active","management_type":"self_directed","affiliate":"rhf","type":"cash","deactivated":false,"has_futures_account":false,"advisory_account_variant":null}],"next":null}`)
				case "/positions/", "/options/positions/":
					fmt.Fprint(w, `{"results":[],"next":null}`)
				case "/portfolios/001/":
					fmt.Fprintf(w, `{"account":"https://api.robinhood.com/accounts/001/","market_value":%q}`, market)
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
			c, _ := NewPortfolioConnection("default", dir)
			target, _ := url.Parse(server.URL)
			c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
			snapshot, err := c.Snapshot(context.Background(), "001")
			if market == "0.00" {
				if err != nil || len(snapshot.Lots) != 0 || snapshot.WriteEligible {
					t.Fatalf("empty: %v", err)
				}
			} else if err == nil {
				t.Fatal("contradictory value accepted")
			}
		})
	}
}

func TestPortfolioSharedSessionTwoAccountIsolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := func(v ...any) {
			if v == nil {
				v = []any{}
			}
			json.NewEncoder(w).Encode(map[string]any{"results": v, "next": nil})
		}
		owner := func(n string) map[string]any {
			return map[string]any{"account_number": n, "account": "https://api.robinhood.com/accounts/" + n + "/", "instrument": "https://api.robinhood.com/instruments/shared/"}
		}
		switch {
		case r.URL.Path == "/accounts/":
			account := func(n string) map[string]any {
				return map[string]any{"account_number": n, "url": "https://api.robinhood.com/accounts/" + n + "/", "brokerage_account_type": "individual", "state": "active", "management_type": "self_directed", "affiliate": "rhf", "type": "margin", "deactivated": false, "has_futures_account": false, "advisory_account_variant": nil}
			}
			page(account("001"), account("002"))
		case r.URL.Path == "/positions/":
			n := r.URL.Query().Get("account_number")
			if n == "" {
				page()
				return
			}
			row := owner(n)
			row["quantity"] = "1"
			page(row)
		case r.URL.Path == "/options/positions/":
			page()
		case strings.HasPrefix(r.URL.Path, "/portfolios/"):
			n := strings.Trim(strings.TrimPrefix(r.URL.Path, "/portfolios/"), "/")
			json.NewEncoder(w).Encode(map[string]any{"account": "https://api.robinhood.com/accounts/" + n + "/", "market_value": "1"})
		case r.URL.Path == "/instruments/shared/":
			fmt.Fprint(w, `{"id":"shared","symbol":"XYZ","type":"stock"}`)
		case strings.HasPrefix(r.URL.Path, "/tax_lots/open/"):
			parts := strings.Split(r.URL.Path, "/")
			n := parts[3]
			row := owner(n)
			row["instrument_id"] = "shared"
			row["open_lot_id"] = "lot-" + n
			row["quantity"] = "1"
			row["tax_cost_basis"] = "2"
			row["open_date"] = "2025-01-01"
			page(row)
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
	c, _ := NewPortfolioConnection("default", dir)
	target, _ := url.Parse(server.URL)
	c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
	for _, account := range []string{"001", "002"} {
		snapshot, err := c.Snapshot(context.Background(), account)
		if err != nil || len(snapshot.Lots) != 1 || snapshot.Lots[0].ID != "lot-"+account {
			t.Fatalf("account fixture failed: %v", err)
		}
	}
	if _, err := c.Snapshot(context.Background(), "003"); err == nil || cliError(err).Code != ErrorBrokerAccountMismatch {
		t.Fatalf("missing account: %v", err)
	}
}

func TestPortfolioRequestDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	c, _ := NewPortfolioConnection("default", t.TempDir())
	target, _ := url.Parse(server.URL)
	c.auth.Client.client.Transport = rewriteTransport{target: target, base: http.DefaultTransport}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := c.portfolioPages(ctx, "/accounts/", nil)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("deadline not bounded: %v", err)
	}
}
