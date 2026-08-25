export type Guide = {
  route: string;
  title: string;
  eyebrow: string;
  summary: string;
  sections: { title: string; body: string; callout?: string; steps?: string[] }[];
};

export const guides: Guide[] = [
  {
    route: '/',
    title: 'Partner API',
    eyebrow: 'TktSync developers',
    summary:
      'Reserve inventory, protect checkout, confirm commercial sales, and manage the tickets your integration created.',
    sections: [
      {
        title: 'A small, authoritative API',
        body: 'TktSync keeps inventory ownership and ticket state in PostgreSQL. Availability is a snapshot; a successful Reservation is the only way to acquire inventory. Partner credentials are event-scoped and never grant access merely because an identifier is known.',
      },
      {
        title: 'Integration lifecycle',
        body: 'Build your integration around explicit state transitions. Payment remains Partner-owned while TktSync protects inventory and records authoritative commercial confirmation.',
        steps: [
          'Read event layout and availability.',
          'Create one atomic Reservation.',
          'Begin checkout before attempting payment.',
          'Confirm a known success, or report a definitive failure.',
          'Retrieve each ticket credential and hosted QR URL.',
          'Embed the TktSync-hosted QR in your own branded ticket and deliver it.',
        ],
      },
      {
        title: 'Hosted QR responsibility boundary',
        body: 'TktSync generates and hosts the QR image for the current active qr1 credential. Your integration owns the surrounding ticket visual design, branding, email/SMS/app delivery, and whether the image is embedded in HTML, a PDF, or an app screen.',
        callout:
          'The hosted QR URL is an opaque presentation capability, not a hosted ticket page. Treat it as bearer material and do not place it in analytics or routine logs.',
      },
      {
        title: 'Start safely',
        body: 'Use the local environment first. Every mutation requires a durable idempotency key; Reservation owner mutations also require the continuation token returned at creation.',
        callout:
          'Never infer ownership from availability, and never report payment failure while the payment outcome is unknown.',
      },
    ],
  },
  {
    route: '/quickstart',
    title: 'Quickstart',
    eyebrow: 'Get a response',
    summary: 'Make an authenticated event request, then create your first atomic Reservation.',
    sections: [
      {
        title: '1. Obtain Partner credentials',
        body: 'Ask a TktSync operator for a Partner credential and event access. Store credentials only in your server-side secret manager. The interactive console below keeps a credential in page memory only and clears it on refresh.',
      },
      {
        title: '2. Read availability',
        body: 'Request caller-contextual availability for an authorized Event. Treat the response as informational and revalidate customer intent through Reservation creation.',
      },
      {
        title: '3. Create a Reservation',
        body: 'Submit all requested offers together with a fresh Idempotency-Key. The transaction is all-or-nothing and returns an opaque Reservation continuation token.',
        callout:
          'Persist the continuation token securely. Do not place it in URLs, analytics, browser storage, or routine logs.',
      },
    ],
  },
  {
    route: '/authentication',
    title: 'Authentication',
    eyebrow: 'Security',
    summary:
      'Partner requests use bearer credentials and are independently authorized for Partner and Event scope.',
    sections: [
      {
        title: 'Bearer credentials',
        body: 'Send Authorization: Bearer <partner-credential> over HTTPS. Credentials identify the Partner, not an end user. Rotate and revoke them through your TktSync operator.',
      },
      {
        title: 'Event scope',
        body: 'A credential can access only Events covered by an active PartnerEventAccess grant. Knowing an Event, Reservation, Sale, or Ticket ID does not grant authority.',
      },
      {
        title: 'Interactive docs privacy',
        body: 'Credentials entered here remain in React memory. They are never written to localStorage, sessionStorage, cookies, URLs, logs, or telemetry. Reloading the page clears them.',
      },
    ],
  },
  {
    route: '/errors',
    title: 'Errors',
    eyebrow: 'Reliability',
    summary:
      'Use HTTP status, the machine-readable error code, and request ID to decide whether to retry or revise intent.',
    sections: [
      {
        title: 'Structured failures',
        body: 'Errors use a JSON envelope with a stable code, human-readable message, and request correlation information. Business conflicts are distinct from authentication, authorization, validation, and transient server failures.',
      },
      {
        title: 'Retry discipline',
        body: 'Retry ambiguous mutation outcomes with the same idempotency key and the same normalized request. Change the key when customer intent changes. Recover Reservation state with GET rather than guessing.',
      },
      {
        title: 'Payment ambiguity',
        body: 'A network timeout is not a definitive payment failure. Observe Reservation state and reconcile the provider outcome before reporting failure or attempting confirmation.',
        callout:
          'False definitive-failure signals can release protected inventory and cause customer harm.',
      },
    ],
  },
  {
    route: '/idempotency',
    title: 'Idempotency',
    eyebrow: 'Reliable mutations',
    summary: 'Every retriable state change requires a caller-generated Idempotency-Key.',
    sections: [
      {
        title: 'Same key, same intent',
        body: 'Keys are scoped to the authenticated Partner and operation. Repeating the same normalized request with the same key returns the same logical result. Reusing a key with different intent is rejected.',
      },
      {
        title: 'Browser console behavior',
        body: 'This console creates a key for each mutation and preserves it across identical retries. Editing material path, query, or body input rotates the key unless you explicitly edit the key yourself.',
      },
    ],
  },
  {
    route: '/pagination',
    title: 'Pagination',
    eyebrow: 'Collections',
    summary:
      'Activity uses stable cursor pagination; cursors are opaque and must be replayed unchanged.',
    sections: [
      {
        title: 'Cursor flow',
        body: 'Pass the returned next_cursor as the next request cursor. Do not parse, synthesize, or persist assumptions about its format. Set a bounded limit appropriate to your workload.',
      },
      {
        title: 'Stable traversal',
        body: 'A cursor identifies a stable position in the ordered result. New activity can appear before your current position without changing the meaning of a cursor already issued.',
      },
    ],
  },
  {
    route: '/webhooks',
    title: 'Webhooks',
    eyebrow: 'Asynchronous delivery',
    summary:
      'Partner webhooks are delivery hints from the authoritative outbox, not a second source of inventory truth.',
    sections: [
      {
        title: 'Delivery model',
        body: 'TktSync delivers subscribed outbox events at least once to operator-configured Partner HTTPS endpoints. Deduplicate by the stable event id. Any 2xx response acknowledges delivery; redirects and every non-2xx response do not.',
      },
      {
        title: 'Verify every signature',
        body: 'Read TktSync-Signature as t=<unix timestamp>,v1=<hex digest>. Compute HMAC-SHA-256 with the endpoint secret over the exact bytes “<timestamp>.<raw request body>”, compare digests in constant time, and reject stale timestamps. During secret rotation the header can contain two v1 values. Deliveries also include stable TktSync-Event-Id and TktSync-Delivery-Id headers.',
      },
      {
        title: 'Retries and recovery',
        body: 'The current worker attempts delivery up to 8 times. Failures use jittered exponential delays beginning at one second and capped at five minutes; the HTTP timeout is deployment-configurable and defaults to five seconds. Exhausted deliveries become dead letters. A missing webhook is never proof that an operation did not occur—retry the idempotent command or query authoritative state.',
      },
      {
        title: 'Authority and ordering',
        body: 'Use synchronous Partner reads to recover current authoritative state. Webhook or realtime messages cannot create ownership, override PostgreSQL state, or replace a successful Reservation command.',
      },
      {
        title: 'Configuration',
        body: 'Webhook endpoint creation, secret rotation, disablement, and subscriptions are operator-managed Admin operations and are intentionally not exposed as Partner API endpoints in this reference.',
      },
    ],
  },
];
