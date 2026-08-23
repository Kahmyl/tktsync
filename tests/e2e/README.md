# Browser E2E

Critical browser behavior is exercised with Playwright and Chromium.

The suite covers:

- Admin operator authentication and a structured administrative workflow.
- Selector capability consumption, authoritative layout rendering, and reservation hold creation.
- Selector realtime invalidation followed by authoritative state refetch.
- Scanner operator authentication plus admitted and duplicate-admission outcomes.

External API and identity-provider boundaries are mocked at the browser network layer. Backend database and concurrency behavior remain covered by the Go integration suites; these browser tests certify the product-facing flows without duplicating those backend suites.
