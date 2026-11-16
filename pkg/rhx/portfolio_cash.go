package rhx

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CashObservation is a fresh account-bound observation. ObservedAt is the
// reader's UTC fetch time, not a broker-supplied cash update time.
type CashObservation struct {
	Balance, Currency string
	ObservedAt        time.Time
}

const ErrorCashUnavailable ErrorCode = "CASH_UNAVAILABLE"

func cashUnavailable() error { return newError(ErrorCashUnavailable, "Brokerage cash unavailable") }

// Cash reads USD uninvested cash for one explicitly selected eligible account.
// It does not read positions or persist cash. See CASH_READER.md for source limits.
func (c *PortfolioConnection) Cash(ctx context.Context, account string) (CashObservation, error) {
	if !portfolioID.MatchString(account) {
		return CashObservation{}, newError(ErrorValidation, "Invalid broker account")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var result CashObservation
	err := c.withSessionLock(ctx, func() error {
		if err := c.refreshSessionLocked(ctx); err != nil {
			return err
		}
		rows, err := c.portfolioPages(ctx, "/accounts/", url.Values{"default_to_all_accounts": {"true"}})
		if err != nil {
			return err
		}
		if _, err = eligiblePortfolioAccount(rows, account); err != nil {
			return err
		}
		result, err = c.cashLocked(ctx, account)
		return err
	})
	if err != nil {
		return CashObservation{}, err
	}
	return result, nil
}

func (c *PortfolioConnection) cashLocked(ctx context.Context, account string) (CashObservation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://bonfire.robinhood.com/accounts/"+account+"/unified/", nil)
	if err != nil {
		return CashObservation{}, cashUnavailable()
	}
	req.Header.Set("Authorization", c.auth.Client.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.auth.Client.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return CashObservation{}, ctx.Err()
		}
		return CashObservation{}, cashUnavailable()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return CashObservation{}, newError(ErrorAuthRequired, "Brokerage authentication required")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return CashObservation{}, newError(ErrorRateLimited, "Brokerage rate limit exceeded")
	}
	if resp.StatusCode != http.StatusOK {
		return CashObservation{}, cashUnavailable()
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if ctx.Err() != nil {
		return CashObservation{}, ctx.Err()
	}
	if err != nil || len(data) >= 4<<20 {
		return CashObservation{}, cashUnavailable()
	}
	var obj map[string]any
	if !uniqueJSON(data) || json.Unmarshal(data, &obj) != nil || obj == nil {
		return CashObservation{}, cashUnavailable()
	}
	n, ok := requiredString(obj, "account_number")
	if !ok {
		return CashObservation{}, cashUnavailable()
	}
	if n != account {
		return CashObservation{}, newError(ErrorBrokerAccountMismatch, "Broker account does not match")
	}
	money, ok := obj["uninvested_cash"].(map[string]any)
	if !ok {
		return CashObservation{}, cashUnavailable()
	}
	amount, ok := requiredString(money, "amount")
	if !ok {
		return CashObservation{}, cashUnavailable()
	}
	unsigned := strings.TrimPrefix(amount, "-")
	if _, valid := exactDecimal(unsigned); !valid {
		return CashObservation{}, cashUnavailable()
	}
	currency, ok := requiredString(money, "currency_code")
	if !ok || currency != "USD" {
		return CashObservation{}, cashUnavailable()
	}
	return CashObservation{Balance: amount, Currency: currency, ObservedAt: time.Now().UTC()}, nil
}

// Reject duplicate object keys, including nested money fields, instead of
// letting a JSON decoder silently select one of conflicting values.
func uniqueJSON(data []byte) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 100 {
			return false
		}
		tok, err := d.Token()
		if err != nil {
			return false
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
