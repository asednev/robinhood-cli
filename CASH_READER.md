# Account cash reader

`PortfolioConnection.Cash(ctx, accountNumber)` reads one explicitly selected
eligible brokerage account. It returns an exact signed decimal string, `USD`,
and the UTC time the response was observed. The time is not a broker cash
update time. Each call makes a new read; it does not store cash or fetch
positions, lots, orders or trades. The connection uses the existing profile
lock and session behavior, including ordinary session renewal when required.

The reader checks the complete account listing against the same individual,
active, self-directed RHF eligibility predicate as the portfolio reader. It
then requests the fixed Bonfire `/accounts/{accountNumber}/unified/` route,
requires the returned account number to match, and accepts only a valid USD
`uninvested_cash` amount. Missing, malformed, mismatched or unsupported data
fails; buying power, equity and withdrawable cash are not substitutes.

Source qualification is bounded. A 2026-11-16 read of one eligible account
returned a matching account number and USD `uninvested_cash` amount. The
account-scoped route and use of that field as uninvested cash are also
documented by the community [client source](https://github.com/kevin1chun/robinhood-for-agents/blob/16b3d800e7f29e9d450f45663d054a7b5adcd4b3/src/server/tools/portfolio.ts)
and [schema](https://github.com/kevin1chun/robinhood-for-agents/blob/16b3d800e7f29e9d450f45663d054a7b5adcd4b3/src/client/types.ts).
The account-bound interpretation of the field is an inference from that route,
response identity and client usage, not a published Robinhood API guarantee.
Robinhood describes [swept cash](https://robinhood.com/us/en/support/articles/high-yield-cash-program/)
as visible in the account. Its public material does not specify this API
field's treatment of every unsettled transaction or cash program. The live
observation did not independently validate zero, negative, non-default,
multi-currency, sweep or unsettled cases; test fixtures are synthetic. Other
account types and missing or non-USD responses are unsupported.
