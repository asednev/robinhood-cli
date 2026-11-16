package rhx

import "testing"

func TestEligiblePortfolioAccountSelection(t *testing.T) {
	account := func(n string) map[string]any {
		return map[string]any{"account_number": n, "url": robinhoodAPIBase + "/accounts/" + n + "/", "brokerage_account_type": "individual", "state": "active", "management_type": "self_directed", "affiliate": "rhf", "type": "margin", "deactivated": false, "has_futures_account": false, "advisory_account_variant": nil}
	}
	t.Run("selects requested row", func(t *testing.T) {
		row, err := eligiblePortfolioAccount([]map[string]any{account("002"), account("001")}, "001")
		if err != nil || row["account_number"] != "001" {
			t.Fatal("did not select exact account")
		}
	})
	for _, tc := range []struct {
		name string
		rows []map[string]any
	}{
		{"missing", []map[string]any{account("002")}},
		{"duplicate", []map[string]any{account("001"), account("001")}},
		{"missing identity", []map[string]any{{}, account("001")}},
		{"wrong reference", []map[string]any{func() map[string]any { a := account("001"); a["url"] = robinhoodAPIBase + "/accounts/002/"; return a }()}},
		{"missing eligibility", []map[string]any{func() map[string]any { a := account("001"); delete(a, "has_futures_account"); return a }()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if row, err := eligiblePortfolioAccount(tc.rows, "001"); err == nil || row != nil {
				t.Fatal("expected failure without a selected row")
			}
		})
	}
}
