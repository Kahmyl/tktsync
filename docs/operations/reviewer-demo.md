# Reviewer demo operations

The Reviewer Hub and Northstar Tickets are assessment tools, not TktSync product surfaces.

## Configure and seed

Set `REVIEW_SEED_ADMIN_TOKEN`, `API_PUBLIC_URL`, and the deployed HTTPS
`PARTNER_DEMO_RETURN_URL`, then run:

```sh
make seed-review-demo
```

The command is idempotent for its generation. It creates Meridian Arena, a published
reserved/table/GA layout, Championship Night, three NGN price tiers, materialized
inventory, Demo Partner access, an allowed checkout-return URL, and (on first run) a
Partner credential. The credential is written mode `0600` to ignored
`.review-demo.env`; move it into the deployment secret `PARTNER_DEMO_CREDENTIAL`.
It does not create reservations, sales, tickets, or admissions.

## Reset strategy

The primary fixture has 156 reserved/table seats and 500 GA admissions, so normal
review traffic cannot exhaust it quickly. To give a new review cohort completely
fresh inventory without deleting audit history, run `make reset-review-demo`. This
creates a new timestamped Event generation and grants the existing Demo Partner
access. No destructive public reset endpoint exists.

## Runtime configuration

The Partner Demo is a server-rendered BFF on Vercel, so its runtime variables must be configured
on the **partner-demo-web project itself**, for Production as well as any assessment Preview
deployment:

| Variable | Required value |
| --- | --- |
| `API_PUBLIC_URL` | Public HTTPS origin of the deployed Go API; do not point this at Reviewer Hub or Admin |
| `PARTNER_DEMO_PUBLIC_URL` | Public Partner Demo origin, with no trailing slash |
| `PARTNER_DEMO_RETURN_URL` | The same origin followed by `/checkout/return` |
| `PARTNER_DEMO_SESSION_SECRET` | Random server-only secret of at least 32 characters |
| `SCANNER_PUBLIC_URL` | Public Scanner origin |
| `REVIEWER_PUBLIC_URL` | Public Reviewer Hub origin |

`API_PUBLIC_URL`, `PARTNER_DEMO_RETURN_URL`, and `PARTNER_DEMO_SESSION_SECRET` are server-only
runtime values. A `VITE_` variable in another Vercel project does not configure this BFF. After
changing them, redeploy Partner Demo and inspect `GET /demo-api/config`: `api_configured` and
`session_configured` must both be `true`, and `return_url` must be the exact URL registered on the
Partner in Admin.

Set `PARTNER_DEMO_SESSION_SECRET` to a strong deployment secret. It encrypts the
HttpOnly cookies that hold saved assessment connections and checkout state, allowing
the Vercel BFF to remain stateless across function instances. Reviewers can enter the
one-time credential issued in Admin on the Partner Demo connection screen; it is
validated server-side and never returned to browser JavaScript. `PARTNER_DEMO_CREDENTIAL`
remains an optional deployment-managed default connection.

`PARTNER_DEMO_RETURN_URL` must exactly match the registered public HTTPS URL.
TLS can terminate at the deployment proxy; the application itself listens on `PORT`.
For a direct local HTTPS callback, set `PARTNER_DEMO_TLS_KEY` and
`PARTNER_DEMO_TLS_CERT`; the Node BFF then serves HTTPS itself.
Reviewer account values are optional build configuration and should only describe a
temporary, least-privilege assessment account.
