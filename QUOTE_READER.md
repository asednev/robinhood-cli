# Quote reader

`rhx.NewQuoteConnection` opens the same profile session used by
`NewPortfolioConnection`. Supply an absolute session directory (or an empty
string for the default), an optional `http.RoundTripper`, and a `QuoteBudget`
with a positive request count, byte count, and absolute deadline. Share one
budget across connections when all their traffic belongs to one invocation.
The budget includes explicit login, refresh, replay, and response bodies.
Do not change its fields while it is in use.

Call `EquityQuotes`, `CurrencyPairs`, and `ForexQuote` for the raw responses.
`QuoteResponse` retains the complete body, status, safe endpoint, retrieval time,
and safe Retry-After. `Rows` extracts exact JSON rows from a results envelope,
including duplicates and nulls; consumers must check status, identity, missing
rows, and response shape. A body that exceeds the budget is not returned.
Consumers are responsible for determining whether market-data bytes are safe
to retain or export. Authentication responses are never returned as captures.

Quote reads use the stored session, without credentials, keyring, account or
position requests. One rejected read may trigger one noninteractive refresh and
replay per shared budget; another rejection is returned to the caller. Explicit
`Login` takes credentials and an optional MFA code, can wait for an interactive
challenge, and has a five-minute maximum deadline. Session writes use the same
profile lock and private atomic replacement as the portfolio facade. Legacy RHX
clients that do not take this lock are not safe concurrent session writers.

Fixture tests do not establish live endpoint availability or field semantics.
