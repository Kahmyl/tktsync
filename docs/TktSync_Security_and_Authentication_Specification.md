# TktSync Security & Authentication Specification

**Document status:** Governing Security & Authentication Specification  
**Applies to:** TktSync MVP, all first-party web surfaces, and compatible Partner integrations  
**Version:** 1.0  
**Date:** 20 August 2026  
**Classification:** Confidential

**Normative parents, in precedence order:**
1. TktSync Platform Process & Policy Standard v1.0
2. TktSync Logical Domain Specification v1.0
3. TktSync System Architecture & Transactional Design Specification v1.0
4. TktSync Relational Data Model & Schema Specification v1.0
5. TktSync API & Partner Integration Contract v1.0
6. TktSync Realtime & Event Contract v1.0

**Product basis:** TktSync Technical Brief (2026)

---

## 1. Purpose

This document defines the authentication, authorization, credential, cryptographic-token, secret-management, browser-security, webhook-signing, and service-security model for TktSync.

It governs:

- Partner machine credentials;
- human administrator and scanner authentication;
- Event role-based authorization;
- BuyerSelectionSession capabilities;
- Reservation continuation tokens;
- QR credential representation and verification;
- realtime stream authorization;
- Partner webhook signing and secret storage;
- token and key rotation;
- security logging/auditing;
- credential revocation;
- browser transport and storage rules;
- cross-Partner isolation;
- security failure behavior.

This specification does not redefine business-state transitions. Authentication proves who/what is calling; authorization determines what that principal may do; the domain/database still determines whether the requested business transition is valid.

---

## 2. Normative Language

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

---

## 3. Governing Security Principle

> **No credential, token, identifier, browser state, or realtime subscription bypasses authoritative domain authorization and PostgreSQL business guards.**

Knowing a resource identifier or possessing a narrow capability does not imply broader platform authority.

---

# PART I — TRUST BOUNDARIES AND PRINCIPALS

## 4. Authentication Classes

TktSync SHALL distinguish the following principal classes:

1. **Partner machine principal** — Partner server-to-server API credential.
2. **Human principal** — authenticated administrator/event staff/scanner.
3. **Buyer selection principal** — time-bounded BuyerSelectionSession capability.
4. **Internal service principal** — API, worker, dispatcher, migration role.

Reservation continuation tokens and QR credentials are additional scoped proofs, not complete actor identities.

Webhook signing secrets authenticate TktSync deliveries to Partners; they are not inbound Partner API credentials.

---

## 5. Authentication vs Authorization

Authentication answers:

> Who or what presented this credential?

Authorization answers:

> Is this authenticated principal allowed to perform this operation for this Event/resource right now?

These MUST remain separate.

Examples:

- a valid Partner credential does not grant access to every Event;
- a valid human JWT does not make the user an Event Manager;
- a valid BuyerSelectionSession capability cannot confirm a Sale;
- a valid Reservation token does not permit another Partner to mutate that Reservation;
- a cryptographically valid QR token does not mean the Ticket is currently admissible.

---

## 6. Least Authority

Each credential type SHALL authorize only its intended surface.

Credential classes MUST NOT be accepted interchangeably.

A Partner API key cannot authenticate as an administrator.

A scanner session cannot perform Event lifecycle changes.

A BuyerSelectionSession cannot access Partner secrets or commercial confirmation endpoints.

---

# PART II — CRYPTOGRAPHIC BASELINE

## 7. Randomness

Security-sensitive random values SHALL be generated using a cryptographically secure random number generator.

Minimum entropy for new bearer secrets SHOULD be 256 bits.

Non-cryptographic random generators MUST NOT be used for:

- Partner API secrets;
- signing secrets;
- one-time security nonces;
- cryptographic root keys.

---

## 8. Encoding

Opaque binary secret material SHOULD use unpadded base64url or another URL/header-safe canonical encoding.

Encoding is not encryption and MUST NOT be treated as secrecy.

---

## 9. Constant-Time Comparison

Secret/MAC/digest verification MUST use constant-time comparison where applicable.

---

## 10. HMAC

Where this specification requires deterministic/recoverable bearer material, the approved primitive is:

~~~text
HMAC-SHA-256
~~~

The HMAC key is versioned, stored outside ordinary PostgreSQL business tables, and available only to the server processes that require it.

---

## 11. Hashing High-Entropy API Secrets

Partner API secrets are high-entropy randomly generated bearer secrets.

The database MAY store:

~~~text
SHA-256(secret)
~~~

for lookup verification because the secret has sufficient entropy to make offline brute-force infeasible.

Raw Partner API secrets MUST NOT be stored after issuance.

If a keyed digest/pepper is introduced later, it requires key-version handling but does not change Partner protocol semantics.

---

## 12. Encryption of Recoverable Server Secrets

Secrets that TktSync itself must later recover in plaintext, such as Partner webhook signing secrets, MUST be encrypted at rest rather than merely hashed.

Approved baseline:

~~~text
AES-256-GCM envelope encryption
~~~

or a managed KMS-equivalent authenticated-encryption mechanism.

The key-encryption/master key MUST be stored outside ordinary application tables in the deployment secret-management system.

Ciphertext records SHALL include the encryption-key version required for rotation.

---

# PART III — PARTNER MACHINE AUTHENTICATION

## 13. Partner Credential Format

Partner API credentials SHALL contain:

- a non-secret lookup key identifier;
- a high-entropy secret.

Representative external format:

~~~text
tkp_<key_id>_<secret>
~~~

The literal prefix is an implementation namespace and MAY include environment labeling if environments are separated.

The Partner MUST treat the complete credential as a secret.

---

## 14. Partner Credential Storage

`partner_credentials` stores:

- Partner identity;
- `key_id`;
- SHA-256 digest of the secret;
- state;
- issuance/revocation metadata.

It MUST NOT store the plaintext secret.

The raw credential is shown only when created/rotated.

---

## 15. Partner Authentication Flow

~~~text
Authorization: Bearer tkp_...
        |
        v
parse key_id + secret
        |
        v
load PartnerCredential
        |
        v
verify credential ACTIVE
        |
        v
SHA-256(presented secret)
        |
constant-time comparison
        |
        v
authenticate Partner identity
        |
        v
apply Partner state + PartnerEventAccess authorization
~~~

The HTTP bearer value MUST NOT be logged.

---

## 16. Partner Credential Rotation

A Partner MAY have multiple active credentials during bounded rotation.

Rotation SHALL:

1. issue a new credential;
2. allow the Partner to deploy it;
3. revoke the old credential explicitly;
4. preserve Partner identity and existing Reservation/Sale history.

Credential revocation is independent of operational Partner disable.

---

## 17. Partner Operational Disable

`partners.state = DISABLED` or disabled PartnerEventAccess blocks new acquisition according to platform policy.

It is not cryptographic credential revocation.

An existing valid credential may still authenticate the Partner identity while authorization denies expansion or permits already-accepted transaction continuation according to policy.

The API MUST preserve the distinction between:

- `PARTNER_DISABLED`;
- `PARTNER_EVENT_ACCESS_DISABLED`;
- invalid/revoked credential.

---

# PART IV — HUMAN AUTHENTICATION

## 18. Human Identity Provider

Human Admin, Event Staff, and Scanner identity SHALL be provided by Supabase Auth or an equivalent standards-based OIDC/JWT identity provider.

For the approved MVP stack, Supabase Auth is the baseline.

TktSync SHALL NOT implement an independent password database.

---

## 19. Human Access Token Verification

The Go Core API SHALL verify human access JWTs using the configured identity-provider signing keys/JWKS.

Verification MUST include:

- signature;
- allowed signing algorithm;
- issuer;
- audience where configured;
- expiry;
- not-before where present.

Algorithm confusion/downgrade MUST be prevented by an explicit allowlist.

---

## 20. Authorization Source

Human JWT claims authenticate identity but MUST NOT be the sole source of Event authorization.

After authentication, the API resolves:

- `app_users.state`;
- `platform_user_roles`;
- `event_staff_assignments`.

Every protected command evaluates current database authorization.

This permits immediate role disable without relying on an old long-lived role claim embedded in a JWT.

---

## 21. Human Roles

The canonical roles remain:

- `PLATFORM_ADMIN`;
- `EVENT_MANAGER`;
- `BOX_OFFICE`;
- `GATE_SUPERVISOR`;
- `SCANNER`;
- `VIEWER`.

The endpoint/command permission matrix SHALL be centralized in the Go authorization layer and tested against the API contract.

UI visibility is not authorization.

---

## 22. Human Session Lifetime

Access-token and refresh-token lifetime are identity-provider configuration.

Access tokens SHOULD be short lived.

Long-lived browser sessions rely on the provider's refresh mechanism rather than excessively long access JWTs.

Revoked/disabled application users remain denied by current database authorization even if a previously issued access JWT has not yet expired.

---

# PART V — BUYER SELECTION CAPABILITY

## 23. BuyerSelectionSession Token Goals

The white-label selector needs a capability that is:

- opaque/unguessable to outsiders;
- bound to one BuyerSelectionSession;
- bound to one Partner;
- bound to one Event;
- time bounded;
- recoverable after an idempotent lost response;
- incapable of Partner/Admin escalation.

---

## 24. Selection Capability Format

The approved deterministic format is logically:

~~~text
sel1.<key_version>.<selection_session_id>.<mac>
~~~

Where:

~~~text
mac = HMAC-SHA-256(
  selection_capability_key[key_version],
  canonical(
    selection_session_id,
    partner_id,
    event_id,
    expires_at
  )
)
~~~

`canonical(...)` MUST use an unambiguous fixed serialization defined in implementation tests.

The token is deterministic for the same session/key version, which allows idempotent response recovery without storing plaintext bearer material.

`buyer_selection_sessions.token_hash` SHALL store `SHA-256(full_encoded_token)` as a defense-in-depth correlation/verifier value, while `token_key_version` identifies the HMAC key required for regeneration.

---

## 25. Selection Capability Verification

Verification SHALL:

1. parse format/version/key version/session ID;
2. load BuyerSelectionSession;
3. verify session state;
4. verify server-authoritative expiry;
5. recompute HMAC from persisted scope;
6. constant-time compare MAC;
7. apply endpoint-level operation authorization.

Cryptographic validity alone is insufficient if the session is `REVOKED` or expired.

Because `expires_at` participates in the deterministic MAC, the session expiry is immutable after capability issuance. A session that needs a later lifetime is replaced by a new BuyerSelectionSession rather than silently extending the existing bearer capability. Revocation may always shorten its effective lifetime.

---

## 26. Selection URL Bootstrap

A selection URL may carry the selection capability in the URL fragment:

~~~text
https://select.<tktsync-domain>/s#sel1....
~~~

The fragment is preferred over a query parameter because it is not sent as part of the HTTP request to the origin server.

The Selector application SHALL:

1. read the fragment immediately;
2. validate/exchange/use it only against the TktSync API;
3. remove it from the visible URL using `history.replaceState` as early as practical;
4. avoid third-party scripts before fragment removal;
5. use `Referrer-Policy: no-referrer` on the bootstrap surface.

The capability MUST NOT appear in analytics events or general logs.

---

## 27. Selection Capability Storage in Browser

Preferred storage is process memory.

If reload resilience requires browser persistence, `sessionStorage` MAY be used after threat review.

`localStorage` SHOULD NOT be used for selection capabilities.

The selection surface SHALL apply a restrictive Content Security Policy to reduce XSS exposure.

---

## 28. Selection Return to Partner

The return destination MUST be pre-registered for the Partner.

Reservation continuation material MUST NOT be appended to a return URL query string.

The approved baseline handoff is an HTTPS form POST or equivalently protected body-based handoff to the registered Partner return destination.

The Partner is still required to authenticate server-side before subsequent Reservation mutation.

---

# PART VI — RESERVATION CONTINUATION TOKEN

## 29. Reservation Token Goals

A successful Reservation returns continuation material that:

- binds to the Reservation;
- binds to Partner/Event scope;
- can be recovered on idempotent replay after a lost response;
- is not sufficient authority by itself;
- is not stored in plaintext.

---

## 30. Reservation Token Format

Approved logical format:

~~~text
rsv1.<key_version>.<reservation_id>.<mac>
~~~

Where:

~~~text
mac = HMAC-SHA-256(
  reservation_token_key[key_version],
  canonical(
    reservation_id,
    partner_id,
    event_id
  )
)
~~~

The database stores `SHA-256(full_encoded_token)` for defense-in-depth/correlation and stores the token key version required for deterministic recovery.

---

## 31. Reservation Token Use

Owner-side Reservation mutations SHOULD require:

1. authenticated owning Partner or owning BuyerSelectionSession authority; and
2. the Reservation continuation token.

The token is transmitted using:

~~~http
X-TktSync-Reservation-Token: <token>
~~~

It MUST NOT be placed in query parameters.

Responses containing the token MUST use:

~~~http
Cache-Control: no-store
~~~

---

## 32. Reservation Token Recovery

When Reservation creation succeeded but its response was lost, retrying the same idempotent operation returns the same logical Reservation and deterministically recreates the usable token from:

- Reservation identity;
- persisted owner/Event scope;
- stored key version;
- server keyring.

No duplicate Reservation is created.

---

## 33. Reservation Token Rotation

Key rotation SHALL preserve old verification keys until every Reservation that depends on them is terminal beyond the configured recovery/retention requirement.

A key MUST NOT be destroyed while active recoverable tokens depend on it.

---

# PART VII — QR CREDENTIAL SECURITY

## 34. QR Credential Goals

QR material SHALL:

- identify one QRCredential;
- be cryptographically unforgeable without server key material;
- remain separate from Ticket identity;
- reveal no unnecessary PII;
- be recoverable for the active credential after a lost response;
- become unusable when the credential is superseded/revoked or Ticket is voided.

---

## 35. QR Payload Format

Approved logical format:

~~~text
qr1.<key_version>.<credential_id>.<mac>
~~~

Where:

~~~text
mac = HMAC-SHA-256(
  qr_credential_key[key_version],
  canonical(
    credential_id,
    ticket_entitlement_id,
    event_id
  )
)
~~~

The QR payload contains no buyer name, email, payment reference, or admission state.

`qr_credentials.token_hash` SHALL store `SHA-256(full_encoded_qr_payload)` while `token_key_version` identifies the HMAC key required for deterministic regeneration/verification.

---

## 36. QR Verification Flow

~~~text
scan QR payload
      |
      v
parse format + credential ID + key version
      |
      v
load QRCredential/Ticket/Event
      |
      v
recompute HMAC + constant-time compare
      |
      v
credential ACTIVE?
      |
Ticket ACTIVE?
      |
correct Event/window?
      |
already admitted?
      |
      v
atomic authoritative Admission transaction
~~~

A valid MAC is only an anti-forgery check.

The database remains the authority for revocation, Ticket state, Event state, and admission history.

---

## 37. QR Recovery

The active QR representation can be recreated deterministically using the credential's persisted identity/scope and key version.

Credential retrieval is read behavior and MUST NOT silently rotate the credential.

---

## 38. QR Reissue

Credential reissue:

- creates a new QRCredential identity;
- marks the old credential superseded/revoked according to the domain transaction;
- uses the currently active QR key version for the new credential;
- preserves TicketEntitlement identity;
- is auditable.

Old cryptographically valid payloads still fail because database credential state is no longer `ACTIVE`.

---

# PART VIII — REALTIME AUTHENTICATION

## 39. Go-Managed Realtime Stream

The preferred MVP browser realtime stream is served by the Go Core API.

It uses the same principal authentication as the caller's normal API surface:

- human bearer JWT for Admin/Scanner;
- BuyerSelectionSession bearer capability for Selector.

No separate broad realtime credential is required.

---

## 40. Stream Authorization

At stream establishment and periodically/when auth changes, the server SHALL verify:

- principal authentication;
- Event scope;
- role/session state;
- current authorization.

If authorization is revoked, the stream SHOULD be terminated promptly.

A client then reauthenticates/reconnects if still eligible.

---

## 41. Optional Supabase Realtime Transport

If direct Supabase Realtime is later used for browser transport:

- channels MUST be private;
- browser clients MUST receive only narrowly scoped receive authority;
- RLS/authorization MUST enforce Event/session scope;
- the Supabase service-role secret MUST NEVER be sent to browsers;
- direct client writes to authoritative inventory tables remain prohibited;
- only sanitized outbox-derived events may be published externally.

The event semantics from the Realtime & Event Contract remain unchanged.

---

# PART IX — PARTNER WEBHOOK SECURITY

## 42. Signing Secret

Each Partner webhook endpoint SHALL have an independently generated high-entropy signing secret.

The secret is used only to authenticate outbound TktSync webhook deliveries.

It is not valid for Partner API authentication.

---

## 43. Webhook Secret Storage

Webhook secrets must be recoverable by TktSync to generate signatures.

They SHALL therefore be stored using authenticated encryption, not a one-way hash.

Each persisted secret record SHALL include:

- encrypted secret ciphertext;
- encryption key version;
- secret lifecycle state;
- activation time;
- retirement/revocation time where applicable.

The master/key-encryption key remains outside ordinary PostgreSQL tables.

---

## 44. Webhook Signature

For each delivery attempt:

~~~text
signed_payload = unix_timestamp + "." + raw_request_body
signature = HMAC-SHA-256(webhook_secret, signed_payload)
~~~

Header:

~~~http
TktSync-Signature: t=<timestamp>,v1=<signature>
~~~

During a bounded secret-rotation overlap, the header MAY contain multiple `v1=` signatures computed over the same timestamp/body with the ACTIVE and RETIRING secrets. A Partner accepts the request when at least one permitted `v1` signature verifies.

The timestamp is the delivery-attempt signing time, not the original event occurrence time.

---

## 45. Partner Verification Requirements

Partner developer documentation SHALL instruct consumers to:

1. read the raw HTTP request body bytes;
2. parse `t` and `v1`;
3. enforce configured timestamp replay tolerance;
4. compute HMAC-SHA-256 over `t + "." + raw_body`;
5. constant-time compare signatures;
6. acknowledge only after the payload has been durably accepted for Partner processing.

JSON reserialization MUST NOT be used before signature verification.

---

## 46. Webhook Replay Tolerance

A bounded timestamp tolerance SHALL be configured and published in the Partner developer documentation.

The default SHOULD be measured in minutes rather than hours.

Because each retry receives a fresh signature timestamp, legitimate delayed webhook retries remain verifiable without accepting indefinitely old signed requests.

---

## 47. Webhook Secret Rotation

Rotation SHALL support a bounded overlap:

- new secret becomes `ACTIVE`;
- previous secret becomes `RETIRING`;
- during overlap, deliveries MAY carry signatures for both secrets so Partner deployment can migrate without downtime;
- after overlap, previous secret is `REVOKED` and no longer signs new attempts;
- endpoint identity and event history remain unchanged.

Secret rotation is auditable.

---

# PART X — BROWSER AND HTTP SECURITY

## 48. TLS

All production application, API, realtime, and webhook traffic MUST use TLS.

Plaintext HTTP MUST redirect or be unavailable according to deployment architecture; bearer secrets MUST never be intentionally transmitted over plaintext HTTP.

---

## 49. CORS

Browser-facing API CORS policy MUST use an explicit allowlist of approved TktSync origins.

Production MUST NOT use wildcard credentialed CORS.

Partner server-to-server API access does not require permissive browser CORS.

---

## 50. CSRF

The approved browser APIs use explicit bearer credentials in the Authorization header rather than ambient cross-site cookies for authoritative commands.

Under that model, CSRF is not the primary request-forgery risk.

If a future browser surface switches authoritative authentication to cookies, it MUST add:

- appropriate `SameSite` cookie policy;
- CSRF token/origin validation;
- reviewed cross-origin behavior.

---

## 51. Content Security Policy

The Selector, Admin, and Scanner applications SHOULD deploy a restrictive CSP.

The Selector bootstrap surface deserves the strictest policy because it temporarily handles a BuyerSelectionSession capability.

Third-party scripts SHOULD be minimized.

Inline/eval-style script execution SHOULD be disabled unless explicitly required and reviewed.

---

## 52. Referrer Policy

Surfaces that may temporarily contain security capability material in a URL fragment SHALL use:

~~~text
Referrer-Policy: no-referrer
~~~

or an equivalently strict policy.

---

## 53. Cache Policy

Responses containing:

- Partner API credentials;
- selection capabilities;
- Reservation continuation tokens;
- QR payload material;
- webhook signing secret material

MUST use `Cache-Control: no-store` where delivered over HTTP.

---

## 54. Framing / Clickjacking

TktSync surfaces SHOULD deny embedding by default unless a specific white-label Partner integration requires embedding.

If embedding is supported, `frame-ancestors` SHALL use an explicit Partner-origin allowlist.

No wildcard framing policy is permitted for privileged Admin/Scanner surfaces.

---

# PART XI — SECRET AND LOG HYGIENE

## 55. Prohibited Logging

The following MUST NOT appear in ordinary application logs, analytics, traces, or client-visible errors:

- full Partner API credentials;
- selection capabilities;
- Reservation continuation tokens;
- raw QR payloads;
- webhook signing secrets;
- Supabase service-role credentials;
- database passwords;
- encryption/HMAC root keys;
- password/reset tokens.

---

## 56. Safe Identifiers in Logs

Logs MAY contain:

- credential `key_id`;
- Partner ID;
- Event ID;
- Reservation ID;
- Ticket ID;
- QRCredential ID;
- request/correlation ID;
- hashed/truncated idempotency identity;
- result/error code.

Logs SHOULD minimize customer PII.

---

## 57. Error Responses

Authentication error responses MUST NOT reveal whether:

- a guessed secret prefix was correct;
- a private resource belongs to another Partner;
- a token MAC failed vs a resource was revoked, where that distinction would create an oracle.

Business-level state codes remain available after the caller is properly authenticated/authorized to know them.

---

# PART XII — KEY MANAGEMENT

## 58. Server Keyrings

TktSync SHALL maintain versioned keyrings for at least:

- BuyerSelectionSession HMAC tokens;
- Reservation continuation HMAC tokens;
- QR credential HMAC tokens;
- webhook secret envelope encryption.

Keys MUST be stored in a deployment secret manager/KMS-equivalent rather than ordinary business tables.

Application configuration references key versions, not raw key material in source control.

---

## 59. Key Version Persistence

Rows whose deterministic token depends on a cryptographic key SHALL persist the key version required to reproduce/verify that token.

At minimum:

- `buyer_selection_sessions.token_key_version`;
- `reservations.continuation_token_key_version`;
- `qr_credentials.token_key_version`.

These are required relational additions identified by this review.

---

## 60. HMAC Key Rotation

Rotation process:

1. add new key version to server keyring;
2. mark it active for new token issuance;
3. keep old key versions verify/recovery-capable;
4. allow dependent sessions/Reservations/credentials to expire or be rotated;
5. retire old version only when no required recoverable object depends on it.

Rotation MUST NOT invalidate active customer rights simply because a deployment key changed.

---

## 61. Emergency Key Compromise

If a bearer-signing/HMAC key is suspected compromised:

- activate a replacement key version immediately;
- identify dependent tokens/credentials;
- revoke/rotate affected narrow credentials where feasible;
- preserve historical Ticket/Sale/Reservation facts;
- record the incident and privileged remediation in audit/security logs.

QR credential key compromise may require credential rotation for affected active Tickets rather than mutating Ticket identities.

---

# PART XIII — INTERNAL SERVICE AND DATABASE SECURITY

## 62. Database Roles

Deployment SHOULD preserve separate database roles for:

- migrations/owner;
- Core API;
- worker;
- outbox/webhook dispatcher;
- read/reporting where needed.

No browser receives a privileged PostgreSQL credential.

---

## 63. Service Secrets

Internal service/database/service-role secrets SHALL be injected through the deployment secret-management facility.

They MUST NOT be committed to the monorepo.

Development `.env` files containing secrets MUST be excluded from source control.

---

## 64. Supabase Service Role

If the Supabase service-role credential is used server-side:

- it remains server-only;
- it MUST NOT be embedded into React builds;
- it MUST NOT be sent to Partner clients;
- authorization must still be implemented in the Go Core API for authoritative commands.

---

# PART XIV — RATE LIMITING AND ABUSE CONTROL

## 65. Rate Limit Dimensions

Rate limits are configuration rather than domain state.

The platform SHOULD rate-limit by appropriate combinations of:

- Partner credential;
- Partner;
- Event;
- BuyerSelectionSession;
- source IP;
- human user;
- scanner operator/device context;
- operation class.

---

## 66. Inventory Hoarding

Security/abuse controls SHALL support the platform anti-hoarding policy through:

- maximum hold quantity;
- maximum active Reservations;
- request-rate limits;
- suspicious repeated contention monitoring.

Rate limiting MUST NOT substitute for transactional inventory guards.

---

## 67. Scan Abuse

Admission endpoints SHOULD protect against extremely high invalid-scan rates without preventing legitimate gate throughput.

Security controls MUST preserve the distinction between:

- a technical retry using the same idempotency identity;
- a distinct duplicate scan;
- invalid credential guessing.

---

# PART XV — SECURITY AUDIT

## 68. Security-Relevant Audit Events

Audit SHOULD include at minimum:

- Partner credential creation/revocation;
- Partner enable/disable;
- PartnerEventAccess enable/disable;
- human role grant/disable;
- BuyerSelectionSession revocation where material;
- webhook endpoint creation/disable;
- webhook signing secret rotation;
- QR credential reissue/revocation;
- privileged Admission override;
- privileged inventory re-release;
- manual webhook replay;
- security key rotation metadata where operationally appropriate.

Raw secret values MUST NOT be included.

---

## 69. Authentication Metrics

The platform SHOULD measure:

- failed Partner authentications;
- revoked credential use attempts;
- disabled Partner authorization denials;
- invalid/expired selection capabilities;
- invalid QR MAC/credential scans;
- human auth failures where visible to TktSync;
- webhook signature/configuration failures reported during integration testing;
- rate-limit rejection rate.

---

# PART XVI — FAILURE BEHAVIOR

## 70. Identity Provider Unavailable

If a new human session cannot be authenticated because the identity provider is unavailable:

- privileged human operations fail safely;
- existing independently verifiable short-lived JWTs MAY continue only according to configured validation capability and current application authorization checks;
- TktSync MUST NOT bypass authentication because the provider is unavailable.

---

## 71. Keyring Unavailable

If a required HMAC/encryption key cannot be loaded safely:

- affected token issuance/verification fails closed;
- TktSync does not generate unsigned fallback credentials;
- unrelated business surfaces may continue if their authority remains safe.

---

## 72. Database Unavailable

Cryptographically valid credentials do not permit irreversible operations if PostgreSQL authority is unavailable.

Inventory/confirmation/admission still fail closed under the architecture rules.

---

## 73. Realtime Authentication Failure

A failed realtime authentication closes/denies the stream.

It does not invalidate an otherwise valid Reservation or Ticket.

The client may continue using authorized synchronous API behavior and reconnect after authentication recovery.

---

# PART XVII — SECURITY TEST REQUIREMENTS

## 74. Partner Credential Tests

Tests SHALL cover:

- valid credential;
- wrong secret with valid key ID;
- unknown key ID;
- revoked credential;
- disabled Partner;
- valid Partner but disabled Event access;
- rotation with two temporarily active credentials;
- no secret leakage in logs/error bodies.

---

## 75. Selection Capability Tests

Tests SHALL cover:

- valid session;
- expired session;
- revoked session;
- modified session ID;
- modified MAC;
- wrong Event/Partner scope;
- replay after idempotent creation response loss;
- capability rejected on Partner/Admin endpoints.

---

## 76. Reservation Token Tests

Tests SHALL cover:

- deterministic recovery using stored key version;
- changed Reservation ID;
- changed Partner scope;
- wrong HMAC;
- other Partner presenting a valid token;
- terminal Reservation mutation guard;
- key rotation with old active Reservation.

---

## 77. QR Tests

Tests SHALL cover:

- valid QR;
- forged MAC;
- wrong Event;
- superseded credential;
- revoked credential;
- voided Ticket;
- credential recovery after lost confirmation response;
- reissue creating a new QR while old QR remains cryptographically parseable but authoritatively invalid.

---

## 78. Human Authorization Tests

For every Admin/Admission command, test:

- each allowed role;
- each denied role;
- correct Event assignment;
- wrong Event assignment;
- disabled app user;
- disabled assignment;
- Platform Admin exception where applicable.

---

## 79. Webhook Signature Tests

Tests SHALL cover:

- exact raw-body verification;
- body modified by one byte;
- invalid signature;
- stale timestamp;
- fresh retry timestamp;
- current secret;
- previous secret during rotation overlap;
- revoked previous secret after overlap.

---

## 80. SSRF Tests

Webhook configuration/delivery tests SHALL attempt:

- loopback;
- private ranges;
- link-local/metadata IPs;
- DNS rebinding simulation where testable;
- redirect to forbidden destination;
- embedded URL credentials;
- non-HTTPS production URL.

All MUST fail according to policy.

---

# PART XVIII — CROSS-DOCUMENT TRACEABILITY

## 81. Technical Brief Alignment

This security model preserves the Technical Brief responsibility boundaries:

- TktSync protects inventory/Ticket/QR/validation authority;
- Partners retain checkout/payment/customer relationship;
- Event owners retain physical security/gate operations;
- multiple Partner integrations remain isolated;
- white-label selection remains scoped rather than receiving Partner credentials;
- scanner remains mobile-web compatible.

---

## 82. Platform Policy Alignment

- every actor preserves inventory/validation invariants;
- Partner transactions remain private;
- PII is minimized;
- administrative authority does not bypass integrity;
- ambiguous authority fails safely;
- ticket identity and credential identity remain separate;
- credential revocation does not erase Ticket history;
- Event cancellation remains business state rather than auth state.

---

## 83. Logical Domain Alignment

Security preserves separate Partner, PartnerCredential, PartnerEventAccess, BuyerSelectionSession, TicketEntitlement, QRCredential, and human role identities.

No credential system collapses these domain concepts.

---

## 84. System Architecture Alignment

- one Go authoritative backend performs authorization;
- PostgreSQL remains business authority;
- browser clients receive no direct authoritative write access;
- security does not introduce distributed ownership locks;
- failure of realtime/auth helper systems does not redefine business state;
- secrets are not used as replacements for database domain guards.

---

## 85. Relational Schema Alignment

This review requires relational support for:

- token key version on BuyerSelectionSession;
- continuation-token key version on Reservation;
- QR token key version on QRCredential;
- encrypted versioned webhook signing secrets;
- Partner webhook endpoints/subscriptions/delivery records.

No raw Partner/selection/Reservation/QR bearer secret is stored in ordinary plaintext.

---

## 86. API Contract Alignment

This document implements the API contract's deferred security details:

- `Authorization: Bearer` Partner credentials;
- Selection API bearer capability;
- human bearer JWT;
- Reservation token header;
- replay-safe token recovery;
- recoverable active QR representation;
- no secret query parameters;
- secure white-label return;
- least authority across surfaces.

---

## 87. Realtime Contract Alignment

Browser realtime uses the same authenticated principal model as the corresponding API surface.

Partner webhooks use independent outbound signing secrets.

No realtime credential grants broader business authority.

---

# PART XIX — REVIEW FINDINGS & REMEDIATIONS

## 88. Review Finding: Random-and-Discarded Tokens Break Idempotent Recovery

The API contract requires lost successful responses to recover usable Reservation/credential material, while the schema intentionally stores token digests rather than plaintext.

**Resolution:** Buyer selection, Reservation continuation, and QR payloads use deterministic HMAC-derived representations with persisted key versions. Plaintext token storage is unnecessary.

---

## 89. Review Finding: A Valid QR MAC Cannot Be the Admission Authority

A signed QR can remain cryptographically valid after its credential is revoked or Ticket is voided.

**Resolution:** MAC verification only prevents forgery. Every scan still resolves QRCredential, Ticket, Event, and Admission state transactionally.

---

## 90. Review Finding: Human JWT Roles Can Become Stale

Embedding Event roles in a long-lived JWT as sole authority would delay role revocation.

**Resolution:** JWT authenticates identity; current `app_users` and `event_staff_assignments` determine command authorization.

---

## 91. Review Finding: Selection Capability in Query Parameters Leaks Easily

**Resolution:** selection bootstrap uses fragment-based capability transport with immediate URL cleanup, strict referrer policy, and no early third-party scripts. Reservation return material uses body-based HTTPS handoff rather than query strings.

---

## 92. Review Finding: Webhook Secrets Cannot Be Hashed Only

TktSync must recover a webhook secret to sign outbound deliveries.

**Resolution:** webhook signing secrets use authenticated encryption at rest with external key management; inbound Partner API secrets remain one-way hashed.

---

## 93. Review Finding: Direct Supabase Table Realtime Would Expand the Trust Surface

Direct browser subscriptions to raw business tables could expose private state and couple UI to persistence.

**Resolution:** preferred MVP realtime is Go-managed sanitized streaming from committed outbox facts. Direct Supabase Realtime remains optional and, if adopted, must use private authorization and sanitized event delivery only.

---

## 94. Review Finding: Webhook Destinations Are a Server-Side Network Boundary

**Resolution:** HTTPS-only production destinations, DNS/IP policy validation, disabled redirects, and SSRF tests are mandatory.

---

# PART XX — NON-NEGOTIABLE SECURITY INVARIANTS

## 95. Security Invariants

1. Authentication and authorization remain separate.
2. Resource knowledge never grants authority.
3. Partner credentials authenticate one Partner only.
4. Partner Event access is evaluated separately.
5. Partner operational disable is not credential revocation.
6. Human JWT authenticates identity but current database roles authorize commands.
7. Scanner authority does not become Event-management authority.
8. BuyerSelectionSession capability never becomes Partner/Admin authority.
9. Reservation token never grants cross-Partner authority by itself.
10. QR cryptographic validity never replaces Ticket/Credential/Admission database validation.
11. Raw Partner API secrets are not stored after issuance.
12. Raw selection, Reservation, and QR bearer values are not stored in ordinary plaintext.
13. Deterministic bearer tokens use versioned HMAC-SHA-256.
14. Key versions remain available while dependent active/recoverable credentials exist.
15. Webhook signing secrets are encrypted at rest because TktSync must recover them.
16. Webhook signing uses HMAC-SHA-256 over timestamp plus raw request body.
17. Secret comparisons use constant-time comparison where applicable.
18. Production traffic carrying credentials uses TLS.
19. Secrets are never placed in ordinary query parameters.
20. Secrets are never written to general logs/analytics/error responses.
21. Browser clients never receive database owner/service secrets.
22. Direct browser authoritative-table writes remain disabled.
23. Cross-Partner private transaction data remains isolated.
24. Security system failure never causes TktSync to guess irreversible business state.
25. Credential/key rotation preserves historical business identity.
26. Security-sensitive privileged actions remain auditable.
27. Webhook URLs are protected against SSRF.
28. Rate limiting supplements but never replaces transaction/authorization invariants.
29. Token response recovery cannot create duplicate business effects.
30. Any future authentication class requires explicit review before gaining domain authority.

---

## 96. Final Security Summary

~~~text
PARTNER BACKEND
  Partner API Key
       |
       v
Partner identity + PartnerEventAccess

ADMIN / SCANNER
  Supabase Auth JWT
       |
       v
app_users + current RBAC/Event assignment

WHITE-LABEL SELECTOR
  BuyerSelectionSession signed capability
       |
       v
narrow Event/session operations only

RESERVATION CONTINUATION
  signed Reservation token
       +
  owning authenticated principal

QR ADMISSION
  signed QR credential
       +
  authoritative Ticket/Credential/Event/Admission state

PARTNER WEBHOOK
  encrypted endpoint signing secret
       |
       v
HMAC-signed outbound delivery
~~~

The governing principle remains:

> **Cryptography establishes authenticity and protects credentials; authorization establishes scope; PostgreSQL/domain rules establish business truth. None substitutes for the others.**

---

**End of Document**
