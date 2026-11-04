# Experimental portfolio reader for Portfolio Pilot

This reader is for synthetic development and read-only qualification. **Do not use its output for real-data writes.** `PortfolioSnapshot.WriteEligible` is always false. Broker basis is unqualified and remains null; available probes also do not establish full account and product coverage. The API's web endpoints and response schemas are not a published stable contract. Account and instrument classifications below are conservative provisional assumptions; unknown shapes fail.

## API

`NewPortfolioConnection(profile, sessionDir)` creates a connection for a path-safe profile (empty selects `default`). An empty directory selects rhx's normal host-local session directory; an explicit directory must be absolute. `Login(ctx, PortfolioCredentials{Username, Password}, waitForChallenge)` injects credentials for explicit authentication without a keyring. Human verification uses the existing interactive challenge flow when enabled. `RefreshSession(ctx)` uses only an existing session and never starts password login or waits for a challenge. `Snapshot(ctx, accountNumber)` also performs session-only authentication and reads one exact account. A `CLIError` code distinguishes `ErrorAuthRequired`, `ErrorBrokerAccountMismatch`, and `ErrorIncompleteSnapshot`. Context cancellation and deadline errors propagate. Do not log credentials, full account numbers, session contents, or raw HTTP responses.

Calls for the same profile through this API serialize across processes using a private lock file; refreshed sessions are replaced atomically with owner-only permissions. Other existing rhx interfaces do not participate in that lock and must not update the same session concurrently. Separate profiles use separate files. Login takes credentials in memory and does not persist them in the keyring. The caller is responsible for secure credential sourcing. The reader rejects redirects and limits authenticated pages to the expected HTTPS API origin and endpoint; it performs no retries and bounds reads by context and request timeouts.

For the provisional snapshot, an account must report `brokerage_account_type` as `individual`, `state` as `active`, and the exact number and account URL. Account, positions, options and lots use paginated `results`/`next` objects. The reader checks scoped equity and options, instrument detail, portfolio `market_value`, and open lots; re-reads positions and options to detect observable drift; and checks each lot sum using exact decimal arithmetic. An empty inventory with nonzero `market_value` is rejected. The supported instrument types are provisionally `stock` and `etp`; a nonzero option or an unknown type fails. This does not yet prove that these routes enumerate every possible security or that these classifications cover all stocks and ETFs. A separately owned crypto account is not read by this API; any account configuration whose coverage is uncertain remains ineligible.

Each lot must report the exact `account_number` and `instrument_id`; if account or instrument URL references are present, they must match too. Position and options rows retain their separate ownership checks.

`PortfolioLot.Quantity` maps the broker's full `quantity`, including reserved shares, not `quantity_available`. `OpenDate` maps `open_date`; explicit null becomes nil, while a missing or malformed date fails. `TaxCostBasis` is always nil: raw `tax_cost_basis` is ignored regardless of its presence or value. No basis is derived from average price, cost per share, orders, or arithmetic. `BasisCurrency` is nil when no recognized currency is supplied; pp applies its USD default. Recognized explicit currency fields are `basis_currency`, `tax_cost_basis_currency`, and `currency`; a non-USD or malformed value fails. Their precise broker schema and the meaning of absent currency require qualification. Exact nonnegative quantities are limited to fewer than 10^20 and at most 18 fractional digits. `ObservedAt` is the UTC completion time; pp would use its UTC date as the observation date.

## Local joint development

Keep the workspace files outside both repositories. From an independent local directory, create a workspace referencing the two existing checkouts, for example:

```sh
mkdir -p /tmp/pp-rhx-workspace
GOWORK=off go work init /absolute/path/to/portfolio-pilot /absolute/path/to/robinhood-cli
```

Run `go work init` with that independent directory as the working directory, then run pp checks with `GOWORK=/tmp/pp-rhx-workspace/go.work`. The workspace and local-path replacements must not be committed to pp. Joint testing and standalone cold-cache release acceptance are pending. The approved publication route is a reviewable branch in the public asednev fork, followed by a versioned remote replacement in pp.

## Qualification needed

- Broker-basis verification is deferred; the first delivery keeps basis null.
- Establish complete enumeration of nonzero securities for each permitted account/product configuration, exact account and instrument classification fields, portfolio valuation semantics, currency representation and empty-account behavior. The prior read-only sample is bounded evidence for one account, not general coverage.
- Complete joint pp/rhx tests and standalone verification. Linux execution is outside the current validation scope. Do not change `WriteEligible` until qualification and any required review are complete.
