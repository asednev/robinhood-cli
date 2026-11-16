# Account cash reader

`PortfolioConnection.Cash(ctx, accountNumber)` attempts a fresh, account-bound
total USD cash read. Total-cash source qualification is incomplete: **no cash
success state is currently enabled**. Even a matching account with a valid USD
`uninvested_cash` response returns `ErrorCashUnavailable` and an empty
`CashObservation`. This is a fail-closed implementation pending qualification,
not completed Robinhood cash support or acceptance evidence for Portfolio Pilot.

The intended result remains an exact signed decimal string, `USD`, and the UTC
time the response was observed, not a broker cash update time. Each call makes
a new read; it does not store cash or fetch positions, lots, orders or trades.
The connection uses the existing profile lock and session behavior, including
ordinary session renewal when required.

The reader checks the complete account listing against the same individual,
active, self-directed RHF eligibility predicate as the portfolio reader. It
then requests the fixed Bonfire `/accounts/{accountNumber}/unified/` route,
requires the returned account number to match, and validates the exact signed
decimal syntax and explicit USD currency of `uninvested_cash`. Those checks
are necessary but do not establish total-cash semantics. Missing, malformed,
mismatched or unqualified data fails; buying power, equity and withdrawable
cash are not substitutes. Identity mismatch, authentication and rate-limit
errors retain their distinct codes. No partial amount or observation time is
returned on error.

## Source evidence and its limits

- The review handoff reports two successful normal-build source observations.
  The original implementation note records a 2026-11-16 response from one
  eligible account with matching identity and a USD `uninvested_cash` amount.
  These observations establish response availability and shape, not the
  field's total-cash meaning or its treatment of unsettled or swept funds.
- The handoff separately reports two failed test-only local probes. These are
  local probe failures, not successful live qualification or evidence that
  the normal-build provider reads failed. Per-observation receipts and probe
  failure diagnostics are not included in this worktree; no further detail
  is inferred and neither category is combined with the other.
- The pinned community [client source](https://github.com/kevin1chun/robinhood-for-agents/blob/16b3d800e7f29e9d450f45663d054a7b5adcd4b3/src/server/tools/portfolio.ts)
  exposes `unified.uninvested_cash.amount` separately from account cash,
  buying power and withdrawable cash. Its [schema](https://github.com/kevin1chun/robinhood-for-agents/blob/16b3d800e7f29e9d450f45663d054a7b5adcd4b3/src/client/types.ts)
  describes a money object but gives no total-cash or settlement definition.
  Route, response identity and client usage support account binding; they do
  not qualify this field as total cash.
- Robinhood's [High-Yield Cash Program](https://robinhood.com/us/en/support/articles/high-yield-cash-program/)
  describes brokerage-held and swept uninvested cash, separately identifies
  options collateral, and limits interest eligibility to settled cash outside
  pending transfers. The [Cash sweep program](https://robinhood.com/us/en/support/articles/deposit-sweep-program/)
  describes access to swept balances through the investing account.
- Robinhood's [Settlement and buying power](https://robinhood.com/us/en/support/articles/settlement-and-buying-power/)
  distinguishes cash and margin accounts' use of unsettled funds. These public
  product descriptions do not map the Bonfire `uninvested_cash` field to total
  cash for either account type. Treating them as that mapping would be an
  inference, not source qualification.

## Supported states and remaining decision

The request and validation path supports selection of active, individual,
self-directed RHF cash and margin accounts with no futures account or advisory
variant. Neither type currently has an established total-cash mapping, so both
remain unavailable for successful cash observations. Zero, negative, settled,
unsettled, sweep-enabled, sweep-disabled, default and non-default cases are not
promoted to qualified success by amount, account type or response shape.
Other account types and missing or non-USD responses remain unsupported.
Synthetic fixtures exercise rejection, not live semantic qualification.

To enable success within the approved contract, obtain attributable evidence
that a selected provider field represents total USD cash for a concrete enabled
account state, including its settlement and sweep treatment, and establish how
the reader identifies that state. No aggregate formula or assumption about
missing state is used. The remaining product decision is whether to pursue
that source evidence or leave Robinhood cash unavailable; the contract has
not been narrowed to uninvested, settled, spendable or withdrawable cash.
No new live probe, Keychain access, login/refresh or broader account read was
performed for this review fix.
