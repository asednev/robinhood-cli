package rhx

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PortfolioLot contains broker-reported values, never inferred from prices.
// TaxCostBasis is nil while broker basis is unqualified. Date may be nil when
// the broker reports null. A nil currency means none was reported.
type PortfolioLot struct {
	ID, Symbol, Quantity                  string
	OpenDate, TaxCostBasis, BasisCurrency *string
}

// PortfolioSnapshot is an observation, not a transactional broker snapshot.
// Account and product coverage require qualification before writes.
type PortfolioSnapshot struct {
	Lots       []PortfolioLot
	ObservedAt time.Time
	// WriteEligible is always false until independent coverage is qualified.
	WriteEligible bool
}

const (
	ErrorBrokerAccountMismatch ErrorCode = "BROKER_ACCOUNT_MISMATCH"
	ErrorIncompleteSnapshot    ErrorCode = "INCOMPLETE_BROKER_SNAPSHOT"
)

var portfolioID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var portfolioDecimal = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

func incomplete() error { return newError(ErrorIncompleteSnapshot, "Incomplete brokerage snapshot") }
func exactDecimal(s string) (*big.Rat, bool) {
	if !portfolioDecimal.MatchString(s) {
		return nil, false
	}
	parts := strings.Split(s, ".")
	if len(parts[0]) > 20 || (len(parts) == 2 && len(parts[1]) > 18) {
		return nil, false
	}
	v, ok := new(big.Rat).SetString(s)
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil)
	if !ok || v.Cmp(new(big.Rat).SetInt(limit)) >= 0 {
		return nil, false
	}
	return v, true
}
func requiredString(m map[string]any, k string) (string, bool) {
	s, ok := m[k].(string)
	return s, ok && s != ""
}
func optionalString(m map[string]any, k string) (*string, bool) {
	v, ok := m[k]
	if !ok {
		return nil, false
	}
	if v == nil {
		return nil, true
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil, false
	}
	return &s, true
}
func accountOwned(row map[string]any, account string) bool {
	n, ok := requiredString(row, "account_number")
	if !ok || n != account {
		return false
	}
	ref, ok := requiredString(row, "account")
	return ok && ref == robinhoodAPIBase+"/accounts/"+account+"/"
}
func lotOwned(row map[string]any, account, instrument string) bool {
	n, nok := requiredString(row, "account_number")
	id, iok := requiredString(row, "instrument_id")
	if !nok || n != account || !iok || id != instrument {
		return false
	}
	for key, expected := range map[string]string{
		"account":    robinhoodAPIBase + "/accounts/" + account + "/",
		"instrument": robinhoodAPIBase + "/instruments/" + instrument + "/",
	} {
		if ref, exists := row[key]; exists && ref != expected {
			return false
		}
	}
	return true
}
func instrumentID(ref string) (string, bool) {
	prefix := robinhoodAPIBase + "/instruments/"
	if !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, "/") {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(ref, prefix), "/")
	return id, portfolioID.MatchString(id)
}

// Snapshot reads one account without using the keyring or password login. All
// returned data remains unqualified for real imports until the caller has
// separately established the documented coverage prerequisites.
func (c *PortfolioConnection) Snapshot(ctx context.Context, account string) (PortfolioSnapshot, error) {
	if !portfolioID.MatchString(account) {
		return PortfolioSnapshot{}, newError(ErrorValidation, "Invalid broker account")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var result PortfolioSnapshot
	err := c.withSessionLock(ctx, func() error {
		if err := c.refreshSessionLocked(ctx); err != nil {
			return err
		}
		var err error
		result, err = c.snapshotLocked(ctx, account)
		return err
	})
	if err != nil {
		return PortfolioSnapshot{}, err
	}
	return result, nil
}
func (c *PortfolioConnection) snapshotLocked(ctx context.Context, account string) (PortfolioSnapshot, error) {
	accounts, err := c.portfolioPages(ctx, "/accounts/", url.Values{"default_to_all_accounts": {"true"}})
	if err != nil {
		return PortfolioSnapshot{}, err
	}
	found := false
	for _, row := range accounts {
		n, ok := requiredString(row, "account_number")
		if !ok {
			return PortfolioSnapshot{}, incomplete()
		}
		if n != account {
			continue
		}
		if found {
			return PortfolioSnapshot{}, incomplete()
		}
		found = true
		typ, tok := requiredString(row, "brokerage_account_type")
		state, sok := requiredString(row, "state")
		ref, rok := requiredString(row, "url")
		if !tok || typ != "individual" || !sok || state != "active" || !rok || ref != robinhoodAPIBase+"/accounts/"+account+"/" {
			return PortfolioSnapshot{}, incomplete()
		}
	}
	if !found {
		return PortfolioSnapshot{}, newError(ErrorBrokerAccountMismatch, "Broker account is not accessible")
	}
	first, err := c.inventory(ctx, account)
	if err != nil {
		return PortfolioSnapshot{}, err
	}
	portfolio, err := c.portfolioObject(ctx, "/portfolios/"+account+"/")
	if err != nil {
		return PortfolioSnapshot{}, err
	}
	ref, ok := requiredString(portfolio, "account")
	if !ok || ref != robinhoodAPIBase+"/accounts/"+account+"/" {
		return PortfolioSnapshot{}, incomplete()
	}
	market, ok := requiredString(portfolio, "market_value")
	value, valid := exactDecimal(market)
	if !ok || !valid || (len(first) == 0 && value.Sign() != 0) {
		return PortfolioSnapshot{}, incomplete()
	}
	lots := []PortfolioLot{}
	seen := map[string]bool{}
	for _, pos := range first {
		detail, err := c.portfolioObject(ctx, "/instruments/"+pos.id+"/")
		if err != nil {
			return PortfolioSnapshot{}, err
		}
		id, iok := requiredString(detail, "id")
		symbol, sok := requiredString(detail, "symbol")
		typ, tok := requiredString(detail, "type")
		if !iok || id != pos.id || !sok || !tok || (typ != "stock" && typ != "etp") {
			return PortfolioSnapshot{}, incomplete()
		}
		if pos.symbol != "" && pos.symbol != symbol {
			return PortfolioSnapshot{}, incomplete()
		}
		rows, err := c.portfolioPages(ctx, "/tax_lots/open/"+account+"/"+id+"/", nil)
		if err != nil {
			return PortfolioSnapshot{}, err
		}
		sum := new(big.Rat)
		for _, row := range rows {
			lotID, ok := requiredString(row, "open_lot_id")
			if !ok || seen[lotID] || !lotOwned(row, account, id) {
				return PortfolioSnapshot{}, incomplete()
			}
			seen[lotID] = true
			qty, ok := requiredString(row, "quantity")
			amount, valid := exactDecimal(qty)
			if !ok || !valid || amount.Sign() <= 0 {
				return PortfolioSnapshot{}, incomplete()
			}
			if raw, exists := row["quantity_available"]; exists {
				available, ok := raw.(string)
				n, valid := exactDecimal(available)
				if !ok || !valid || n.Cmp(amount) > 0 {
					return PortfolioSnapshot{}, incomplete()
				}
			}
			sum.Add(sum, amount)
			date, ok := optionalString(row, "open_date")
			if !ok {
				return PortfolioSnapshot{}, incomplete()
			}
			if date != nil {
				if _, err := time.Parse("2006-01-02", *date); err != nil {
					return PortfolioSnapshot{}, incomplete()
				}
			}
			var currency *string
			for _, key := range []string{"basis_currency", "tax_cost_basis_currency", "currency"} {
				if raw, exists := row[key]; exists && raw != nil {
					s, ok := raw.(string)
					if !ok || s != "USD" {
						return PortfolioSnapshot{}, incomplete()
					}
					currency = &s
				}
			}
			lots = append(lots, PortfolioLot{ID: lotID, Symbol: symbol, Quantity: qty, OpenDate: date, BasisCurrency: currency})
		}
		if sum.Cmp(pos.qty) != 0 {
			return PortfolioSnapshot{}, incomplete()
		}
	}
	second, err := c.inventory(ctx, account)
	if err != nil {
		return PortfolioSnapshot{}, err
	}
	if len(first) != len(second) {
		return PortfolioSnapshot{}, incomplete()
	}
	for i := range first {
		if first[i].id != second[i].id || first[i].symbol != second[i].symbol || first[i].qty.Cmp(second[i].qty) != 0 {
			return PortfolioSnapshot{}, incomplete()
		}
	}
	return PortfolioSnapshot{Lots: lots, ObservedAt: time.Now().UTC()}, nil
}

type portfolioPosition struct {
	id     string
	symbol string
	qty    *big.Rat
}

func (c *PortfolioConnection) inventory(ctx context.Context, account string) ([]portfolioPosition, error) {
	query := url.Values{"account_number": {account}, "nonzero": {"true"}}
	options, err := c.portfolioPages(ctx, "/options/positions/", query)
	if err != nil {
		return nil, err
	}
	for _, row := range options {
		if !accountOwned(row, account) {
			return nil, incomplete()
		}
		qty, ok := requiredString(row, "quantity")
		n, valid := exactDecimal(qty)
		if !ok || !valid || n.Sign() != 0 {
			return nil, incomplete()
		}
	}
	rows, err := c.portfolioPages(ctx, "/positions/", query)
	if err != nil {
		return nil, err
	}
	out := []portfolioPosition{}
	seen := map[string]bool{}
	for _, row := range rows {
		if !accountOwned(row, account) {
			return nil, incomplete()
		}
		qty, ok := requiredString(row, "quantity")
		n, valid := exactDecimal(qty)
		if !ok || !valid {
			return nil, incomplete()
		}
		if n.Sign() == 0 {
			continue
		}
		ref, ok := requiredString(row, "instrument")
		id, valid := instrumentID(ref)
		if !ok || !valid || seen[id] {
			return nil, incomplete()
		}
		seen[id] = true
		symbol := ""
		if raw, exists := row["symbol"]; exists {
			var ok bool
			symbol, ok = raw.(string)
			if !ok || symbol == "" {
				return nil, incomplete()
			}
		}
		out = append(out, portfolioPosition{id: id, symbol: symbol, qty: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}
func (c *PortfolioConnection) portfolioObject(ctx context.Context, path string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robinhoodAPIBase+path, nil)
	if err != nil {
		return nil, incomplete()
	}
	req.Header.Set("Authorization", c.auth.Client.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.auth.Client.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, incomplete()
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, newError(ErrorAuthRequired, "Brokerage authentication required")
	}
	if resp.StatusCode != 200 {
		return nil, incomplete()
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil || len(data) >= 4<<20 {
		return nil, incomplete()
	}
	var obj map[string]any
	if json.Unmarshal(data, &obj) != nil || obj == nil {
		return nil, incomplete()
	}
	return obj, nil
}
func (c *PortfolioConnection) portfolioPages(ctx context.Context, path string, query url.Values) ([]map[string]any, error) {
	next := robinhoodAPIBase + path
	if len(query) > 0 {
		next += "?" + query.Encode()
	}
	seen := map[string]bool{}
	rows := []map[string]any{}
	for pages := 0; next != ""; pages++ {
		if pages >= 100 || seen[next] {
			return nil, incomplete()
		}
		seen[next] = true
		u, err := url.Parse(next)
		if err != nil || u.Scheme != "https" || u.Host != "api.robinhood.com" || u.User != nil || u.Path != path || u.Fragment != "" {
			return nil, incomplete()
		}
		// Subsequent pages may add a cursor but must retain the account scope.
		for k, values := range query {
			if strings.Join(u.Query()[k], "\x00") != strings.Join(values, "\x00") {
				return nil, incomplete()
			}
		}
		obj, err := c.portfolioObject(ctx, u.RequestURI())
		if err != nil {
			return nil, err
		}
		raw, ok := obj["results"].([]any)
		if !ok {
			return nil, incomplete()
		}
		for _, v := range raw {
			row, ok := v.(map[string]any)
			if !ok || row == nil {
				return nil, incomplete()
			}
			rows = append(rows, row)
		}
		v, exists := obj["next"]
		if !exists {
			return nil, incomplete()
		}
		if v == nil {
			next = ""
		} else {
			s, ok := v.(string)
			if !ok || s == "" {
				return nil, incomplete()
			}
			next = s
		}
	}
	return rows, nil
}
