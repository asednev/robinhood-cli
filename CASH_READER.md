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
missing state is used. The remaining requirement is source evidence through
a provider field definition or attributable calculation that establishes the
mapping. The user has required that work;
leaving cash unavailable does not satisfy acceptance. The contract has not
been narrowed to uninvested, settled, spendable or withdrawable cash.
No new live probe, Keychain access, login/refresh or broader account read was
performed for this review fix.

## Follow-up source audit

The test-phase follow-up inspected public, unauthenticated provider bundles
on 2026-11-16 UTC. The [login page](https://robinhood.com/login/) identified
production release `2026.46.454+140ed33d0a6`; its
[runtime](https://cdn.robinhood.com/assets/generated_assets/webapp/runtime-06784f58c03e1a5f.js)
identified the lazy-loaded chunk revisions below. These are source reads,
not additional account observations, and do not change the two successful
normal-build observations or the two failed test-only probes above.

| Provider source revision | Traced behavior | Qualification limit |
| --- | --- | --- |
| [App-72eed85ebeccf660.js](https://cdn.robinhood.com/assets/generated_assets/webapp/App-72eed85ebeccf660.js), module `644744`, `UnifiedAccountsRecord` | Uses `/accounts/{account_number}/unified/`, declares `uninvested_cash` as a money field, and maps `buyingPower` to `account_buying_power`. Its `investingValue` adds market value and options collateral, capped by portfolio equity. | Confirms the selected route and distinguishes other metrics; does not define the calculation or settlement/sweep coverage of `uninvested_cash`. The investing-value calculation is not a total-cash formula. |
| [CashManagementPageSwitch-40502ce64c931c5f.chunk.js](https://cdn.robinhood.com/assets/generated_assets/webapp/CashManagementPageSwitch-40502ce64c931c5f.chunk.js), module `62691` | Fetches `UnifiedAccountsRecord` for the account and renders `buyingPower.amount` under the legacy `Cash` or `Cash + Margin` heading. | A `Cash` display label alone is insufficient: this path explicitly uses buying power, which the approved contract excludes. |
| [AccountPage-74565ebf48def954.chunk.js](https://cdn.robinhood.com/assets/generated_assets/webapp/AccountPage-74565ebf48def954.chunk.js), module `780245`, `SweepDetailsTable` and `buildCashBreakdownSubRows` | Renders `timelineSummary.sweep_balance.amount` as cash earning interest, with separate `uninvested_cash_balance.amount` and `options_collateral_balance.amount` subrows. Modal copy explains the brokerage-held/swept split and warns that updates can take two or more business days. | These are interest-timeline fields, not the selected unified field; the labels do not establish an equivalence. The delayed interest balance cannot establish a fresh total-cash observation. |
| [Legend main.rs16623b9a1ec54d02.js](https://cdn.robinhood.com/assets/generated_assets/black-widow/main.rs16623b9a1ec54d02.js), module `14689` | Converts performance-summary `cash`, `pendingDeposits`, `earlyAccessAmount` and `futuresCash` separately into money values. | This is another response contract, not a definition of unified `uninvested_cash`; no equality or substitute formula is established. |

SHA-256 of the downloaded source bytes, in table order:

```text
c94163000d618177c2db76832e1cb76c7288898fb62b122e354074aedd753963
347c453545bc86e60bccab174b4e276a34a041b155c9ee365bc5847ce2f2780d
73d7e9e01731c29bb8baa2a1bf069a70bbe3bb335ccdea6e2ca44555958b76d5
e57d78eb8f4315fdc0629c3277117748a7c4a502ea9185bd53377507bd1ba7be
```

The pinned community client above was also read directly at revision
`16b3d800e7f29e9d450f45663d054a7b5adcd4b3`: `portfolio.ts`
copies `unified?.uninvested_cash?.amount` to the identically named summary
field, while `types.ts` only models its money shape. Neither supplies the
missing provider calculation. Robinhood's public High-Yield Cash Program and
settlement descriptions explain product concepts but do not tie them to this
wire field. This audit therefore found no supported state to enable honestly.

The narrow blocker remains the selected field's coverage of unsettled sale
credits, purchase debits, reserved/collateral cash and brokerage-held versus
swept cash for a concrete eligible account state. Required evidence is a
provider definition or attributable field calculation establishing that
coverage, plus response fields that identify any necessary state restriction.
No hypothetical live account shapes or broker update timestamp are required.
No success flag, invented sum, buying-power fallback or new provider endpoint
was added. T1 and test-2 remain unresolved; this document is an evidence
update, not a waiver or completion of cash support.

The named regression
`TestCashAccountBound/margin_unknown_state/unqualified_positive_USD` passed
at the assigned target before and after the documentation edit with
`go test -count=1 -vet=off ./pkg/rhx
-run '^TestCashAccountBound/margin_unknown_state/unqualified_positive_USD$'
-json`. It exercises the actual `Cash` path against loopback HTTP and asserts
an empty observation with `ErrorCashUnavailable`. There is no failing test
assertion to repair in that case: the outstanding failure is the absent
qualified product success path, which cannot be enabled from this evidence.
No source or test behavior was changed. No full suite, linter, formatter or
static analysis ran. Downloaded research files were removed after inspection.
