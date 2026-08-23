# Local product smoke checklist

1. Copy `.env.example` to `.env`, add external Supabase Auth values when operator login is required, then run `make local-up`. Confirm `make local-ps` shows healthy PostgreSQL, API, Admin, Selector, and Scanner; a running worker; and a successfully exited migration service.
2. Open Admin at `http://localhost:54470`. Authenticate, confirm the structured workflow UI appears, and create/read one harmless test entity if your role permits. Confirm there is no pasted bearer-token UI.
3. Open Selector at `http://localhost:54471` with a valid test selection capability. Confirm the URL fragment is consumed, the venue/seat layout loads, unavailable seats are disabled, a reserved-seat selection stays at quantity 1, and a hold can be created and released. Check GA quantity when fixture data exists.
4. Open Scanner at `http://localhost:54472`. Authenticate and select the event scope. Scan a valid test credential and observe `ADMITTED`; scan it again and observe `TICKET ALREADY ADMITTED`. Confirm authority failures remain fail-closed.
5. With Selector open, change relevant event or availability state and confirm realtime invalidation/refetch updates the browser when `REALTIME_ENABLED=true`.
6. Run `make local-down`, then `make local-up`; confirm PostgreSQL data persists.
7. Run `make local-reset`, acknowledging that it intentionally destroys the local database volume. Confirm migrations run automatically and the full stack becomes ready again.
