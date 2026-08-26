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

The Partner Demo must receive its Partner credential only as a server environment
secret. `PARTNER_DEMO_RETURN_URL` must exactly match the registered public HTTPS URL.
TLS can terminate at the deployment proxy; the application itself listens on `PORT`.
For a direct local HTTPS callback, set `PARTNER_DEMO_TLS_KEY` and
`PARTNER_DEMO_TLS_CERT`; the Node BFF then serves HTTPS itself.
Reviewer account values are optional build configuration and should only describe a
temporary, least-privilege assessment account.
