# Account cash reader

`PortfolioConnection.Cash(ctx, accountNumber)` returns a fresh, account-bound
USD cash balance. For Portfolio Pilot, the user explicitly defined Robinhood's
`uninvested_cash` as the cash balance. The reader returns that field's exact
signed `amount` as `CashObservation.Balance`, `USD` as `Currency`, and the
reader's UTC fetch time as `ObservedAt`. No special measure label or
qualification warning is added. The timestamp is not a broker cash update time.

Each call makes a new read; it does not store cash or fetch positions, lots,
orders or trades. Authentication and locking follow the shared
[portfolio connection contract](PORTFOLIO_READER.md#api), including persistence
of renewed sessions when required.

The reader checks the complete account listing against the shared
[account eligibility rules](PORTFOLIO_READER.md#api). It then requests Bonfire's
fixed `/accounts/{accountNumber}/unified/` route, requires the returned account
number to match, and validates the exact signed decimal syntax and explicit USD
currency of `uninvested_cash`. The amount permits an optional minus sign,
at most 20 integer digits and 18 fractional digits, and an absolute value below
10^20. Its original decimal string is preserved; exponent and plus-sign forms
are rejected.

Missing or malformed cash, ambiguous JSON, missing identity and non-USD currency
return `ErrorCashUnavailable`. At the cash endpoint, identity mismatch,
authentication and rate-limit errors retain their distinct codes. Account-list
validation retains the portfolio reader's errors. No partial amount or
observation time is returned on error. Buying power, equity and withdrawable
cash are not substitutes. Settlement, collateral and sweep fields are not
additional gates or inputs to a derived amount.

## Evidence and superseded qualification failures

- The review handoff reports two successful normal-build source observations.
  The original implementation note records a 2026-11-16 response from one
  eligible account with matching identity and a USD `uninvested_cash` amount.
  These establish response availability and shape. They are not accounting
  proofs or additional live validation of the current implementation.
- The handoff separately reports two failed test-only local probes. These
  remain local probe failures, separate from the successful normal-build
  source observations. Per-observation receipts and failure diagnostics are
  not included in this worktree; no further detail is inferred.
- The pinned community [client source](https://github.com/kevin1chun/robinhood-for-agents/blob/16b3d800e7f29e9d450f45663d054a7b5adcd4b3/src/server/tools/portfolio.ts)
  exposes `unified.uninvested_cash.amount`; its
  [schema](https://github.com/kevin1chun/robinhood-for-agents/blob/16b3d800e7f29e9d450f45663d054a7b5adcd4b3/src/client/types.ts)
  models the money object. The earlier source audit traced the same route and
  money field in provider release `2026.46.454+140ed33d0a6`,
  `App-72eed85ebeccf660.js`, module `644744` (`UnifiedAccountsRecord`).
  These sources support the wire shape; they did not independently resolve
  settlement, sweep or collateral accounting semantics.
- Earlier test rounds truthfully found that both eligible cash and margin
  accounts always returned `CASH_UNAVAILABLE`, including matching-account,
  valid-USD responses. The rejection tests passed while the required product
  success scenario failed. Those failures and the demand for independent
  accounting qualification are superseded by the user's explicit decision:
  "For PP21, Robinhood's uninvested cash is all we need."
  The reader now uses the accepted field directly, without a source-proof gate,
  invented aggregation or alternative provider measure.

Focused loopback tests exercise the public `Cash` path for exact positive,
zero, negative and high-precision amounts, fresh repeated calls, account and
currency binding, malformed responses and service failures. They check that
only the expected GET routes are used and that, with a valid session, cash reads
leave session bytes unchanged and create no cash files. These are controlled
product fixtures, not additional broker observations. No live broker probe,
Keychain access, new login or forced refresh is needed to verify the accepted mapping.
