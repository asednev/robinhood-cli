# Quote reader

`rhx.NewQuoteConnection(profile, sessionDir, transport, budget)` follows the
[portfolio reader's profile, platform, locking, and session storage rules](PORTFOLIO_READER.md#api).
Supply an optional `http.RoundTripper` and a `QuoteBudget` with a positive
request count, byte count, and absolute deadline. Share one budget across
connections when all their traffic belongs to one invocation.
The budget includes explicit login, refresh, replay, and response bodies.
Do not change its fields while it is in use.

Call `EquityQuotes`, `CurrencyPairs`, and `ForexQuote` for the raw responses.
`CurrencyPairsForSymbol` requests metadata for one symbol using a single
encoded query parameter; it does not fall back to the unfiltered endpoint.
Callers must check exact symbol, base and quote identities. An empty response
does not establish absence, and a response with a next page is incomplete.
The raw body retains pagination information; this API does not follow next URLs.
`QuoteResponse` retains the complete body, status, safe endpoint, retrieval time,
and safe Retry-After. `Rows` extracts exact JSON rows from a results envelope,
including duplicates and nulls; consumers must check status, identity, missing
rows, and response shape. A body that exceeds the budget is not returned.
Consumers are responsible for determining whether market-data bytes are safe
to retain or export. Authentication responses are never returned as captures.

Quote reads use the stored session, without credentials, keyring, account or
position requests. A 401 or 403 may trigger one noninteractive refresh and replay
per shared budget; callers must inspect both status and error if access remains
rejected. Explicit `Login` takes `QuoteCredentials` with an optional `MFACode`,
can wait for an interactive challenge, and has a five-minute maximum deadline
subject to the earlier caller or budget deadline.

Fixture tests do not establish live endpoint availability or field semantics.
