package rhx

// eligiblePortfolioAccount selects exactly the requested supported account.
// Account eligibility alone does not establish the meaning of any cash field.
func eligiblePortfolioAccount(accounts []map[string]any, account string) (map[string]any, error) {
	var selected map[string]any
	for _, row := range accounts {
		n, ok := requiredString(row, "account_number")
		if !ok {
			return nil, incomplete()
		}
		if n != account {
			continue
		}
		if selected != nil {
			return nil, incomplete()
		}
		typ, tok := requiredString(row, "brokerage_account_type")
		state, sok := requiredString(row, "state")
		ref, rok := requiredString(row, "url")
		management, mok := requiredString(row, "management_type")
		affiliate, aok := requiredString(row, "affiliate")
		accountType, cok := requiredString(row, "type")
		deactivated, dok := row["deactivated"].(bool)
		futures, fok := row["has_futures_account"].(bool)
		advisory, vok := row["advisory_account_variant"]
		if !tok || typ != "individual" || !sok || state != "active" || !rok || ref != robinhoodAPIBase+"/accounts/"+account+"/" ||
			!mok || management != "self_directed" || !aok || affiliate != "rhf" || !cok || (accountType != "cash" && accountType != "margin") ||
			!dok || deactivated || !fok || futures || !vok || advisory != nil {
			return nil, incomplete()
		}
		selected = row
	}
	if selected == nil {
		return nil, newError(ErrorBrokerAccountMismatch, "Broker account is not accessible")
	}
	return selected, nil
}
