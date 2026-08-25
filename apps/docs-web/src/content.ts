export type Guide = {
  route: string;
  title: string;
  eyebrow: string;
  summary: string;
  sections: {
    title: string;
    body: string;
    callout?: string;
    steps?: string[];
    flow?: { label: string; title: string; body: string; route?: string }[];
    code?: { label: string; value: string };
    links?: { label: string; route: string }[];
  }[];
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
        body: 'Use the Partner credential and Event access supplied during your integration onboarding. Store the credential only in your server-side secret manager. The interactive console below keeps a credential in page memory only and clears it on refresh.',
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
    route: '/workflows',
    title: 'Build ticket sales with TktSync',
    eyebrow: 'Start here · The complete product journey',
    summary:
      'You are building an event storefront for your customers. This guide explains every screen and server connection you need, in the order a customer will use them.',
    sections: [
      {
        title: 'The simple idea',
        body: 'Your website or app sells tickets for Events available through the TktSync Partner API. Your customer opens your product, browses Events, chooses an Event, selects tickets, pays through your checkout, and receives a ticket from you. TktSync works underneath your product to supply Event information, protect inventory, and issue valid tickets.',
      },
      {
        title: 'Who is this documentation for?',
        body: '“Partner” means your company and the server-side application you are building. “Customer” or “buyer” means the person purchasing tickets through your website or app. Those are the only roles your integration needs to model in this guide.',
        callout:
          'Your customer never receives your Partner API credential. All Partner API calls are made by your server. Your browser talks to your server, and your server talks securely to TktSync.',
      },
      {
        title: 'What your company needs to build',
        body: 'TktSync is not your complete public ticket shop. Your company supplies the customer-facing experience and payment system. A normal integration needs the following pieces:',
        steps: [
          'An Events page that lists Events your company is allowed to sell.',
          'An Event page with information and a clear “Choose tickets” or “Buy tickets” button.',
          'A server route that asks TktSync to start ticket selection for that Event.',
          'A secure checkout-return route that receives the customer’s held tickets from TktSync.',
          'Your checkout page and payment-provider integration.',
          'Server logic that tells TktSync whether payment succeeded or definitively failed.',
          'A ticket page, email, PDF, or app screen that shows the issued QR code to the customer.',
          'Order history and customer-support tools appropriate for your product.',
        ],
      },
      {
        title: 'What TktSync supplies underneath your product',
        body: 'TktSync supplies the authoritative Event catalogue available to your Partner, venue layouts, current prices and availability, an optional customer-facing seat Selector, temporary ticket holds, the Reservation lifecycle, ticket issuance, and QR credentials. Your product should use those capabilities rather than creating its own competing inventory state.',
      },
      {
        title: 'What the customer experiences',
        body: 'This is the complete customer journey. The technical API names are included only after explaining what the customer is doing.',
        flow: [
          {
            label: '1 · Customer visits your product',
            title: 'Show Events for sale',
            body: 'Your Events page calls your server. Your server uses List accessible events and returns customer-friendly Event cards with names, dates, venues, and sale status.',
            route: '/api/events',
          },
          {
            label: '2 · Customer chooses an Event',
            title: 'Show Event details',
            body: 'Use the Event ID returned by the list API to retrieve details. The ID is passed by your application; the customer should not need to find or type it.',
            route: '/api/events/retrieve',
          },
          {
            label: '3 · Customer presses “Choose tickets”',
            title: 'Start the TktSync seat Selector',
            body: 'Your server creates a Selection session for the chosen Event and redirects the customer to the returned selection_url.',
            route: '/api/selection-sessions/create',
          },
          {
            label: '4 · Customer chooses tickets',
            title: 'TktSync displays the venue',
            body: 'The customer chooses available seats, tables, or general-admission quantities. TktSync creates a temporary hold so another buyer cannot take the same inventory.',
          },
          {
            label: '5 · Customer presses “Continue to checkout”',
            title: 'Return the held tickets to your product',
            body: 'The browser securely POSTs a Reservation ID and secret Reservation token to the checkout-return route you registered. Your server saves both and opens your checkout page.',
            route: '/workflows/selector',
          },
          {
            label: '6 · Customer pays your company',
            title: 'Run your payment checkout',
            body: 'Before charging, your server begins checkout with TktSync. Your payment provider then handles card, bank, wallet, or other payment methods. TktSync does not charge the customer.',
            route: '/api/reservations/begin-checkout',
          },
          {
            label: '7 · Payment succeeds',
            title: 'Confirm the sale with TktSync',
            body: 'Your server confirms the Reservation. TktSync creates the Sale and returns the Ticket IDs. This is when the temporary hold becomes a completed ticket purchase.',
            route: '/api/reservations/confirm',
          },
          {
            label: '8 · Customer receives tickets',
            title: 'Display or send the issued QR tickets',
            body: 'For each Ticket ID, your server retrieves the current QR. Put it inside your branded ticket page, email, PDF, or app screen and send it to the customer.',
            route: '/workflows/tickets',
          },
        ],
      },
      {
        title: 'The small dictionary you need',
        body: 'These names describe the records passed between stages. An Event is what the customer wants to attend. A Selection session is a temporary permission to open the TktSync Selector for one Event. A Reservation is the customer’s temporary ticket hold. A Reservation token is the secret that proves your checkout owns that hold. A Checkout attempt protects the Reservation while payment is being decided. A Ticket is issued only after confirmation. A QR credential is the machine-readable proof attached to an active Ticket.',
      },
      {
        title: 'The two ways to collect ticket choices',
        body: 'Most Partners should begin with the TktSync Selector because it already understands venue geometry, reserved seats, tables, availability, and general admission. Advanced Partners may build their own selection interface by reading layout and availability and creating Reservations directly.',
        links: [
          { label: 'Recommended: use the TktSync Selector', route: '/workflows/selector' },
          { label: 'Advanced: build your own selection UI', route: '/workflows/direct' },
        ],
      },
      {
        title: 'Continue when the basic journey is clear',
        body: 'The next guides zoom into the stages that require careful server implementation. Start with Selector to checkout, then Ticket delivery. Read retries and recovery before accepting real payments.',
        links: [
          { label: '1. Selector to checkout', route: '/workflows/selector' },
          { label: '2. Deliver a visible ticket', route: '/workflows/tickets' },
          { label: '3. Retries and recovery', route: '/workflows/recovery' },
        ],
      },
    ],
  },
  {
    route: '/workflows/selector',
    title: 'Add ticket selection and checkout',
    eyebrow: 'Step by step · After the customer chooses an Event',
    summary:
      'Your customer has chosen an Event in your website or app. Now let them choose tickets, return them to your checkout, take payment, and complete the order.',
    sections: [
      {
        title: 'Where this workflow begins',
        body: 'Your product has already called List accessible events and displayed those Events to the customer. The customer clicked one Event, and your application kept the Event ID returned by TktSync. When the customer presses your “Choose tickets” button, send that Event ID to your server. Your server—not the browser—uses it to create the Selection session described below.',
        links: [
          { label: 'First build your Events list', route: '/api/events' },
          { label: 'See the complete customer journey', route: '/workflows' },
        ],
      },
      {
        title: 'What “Selector” means',
        body: 'Selector is the TktSync page where your customer sees the configured venue and chooses available tickets. It can display reserved seats, table seats, and general-admission areas. You do not need to recreate that venue interface in your own application. Your application only starts the Selector and receives the customer back when ticket selection is complete.',
      },
      {
        title: 'What you build',
        body: 'You build a server-side checkout return endpoint and your own payment page. TktSync supplies the venue Selector. The return endpoint is essential: it is where the browser delivers the Reservation ID and Reservation token after the buyer presses “Continue to checkout.” It must be a real HTTPS endpoint in your application, not a success page on another website.',
        callout:
          'The return endpoint receives a form POST. The Reservation token is intentionally absent from the browser URL, query string, and fragment. Looking at the address bar will never reveal it.',
      },
      {
        title: 'Before the first buyer arrives',
        body: 'Complete Partner onboarding before sending customer traffic. You should receive a server credential, access to the Events your application may sell, and confirmation that every checkout return URL you supplied has been registered. Keep the Partner credential on your server. Browser code must never contain it.',
        steps: [
          'Create an HTTPS route such as https://checkout.partner.example/tktsync/return.',
          'Supply that exact return URL through your Partner integration onboarding channel and confirm it is registered.',
          'Store the Partner credential in your server-side secret manager.',
          'Use List accessible events to discover Events your Partner can actually sell.',
        ],
        links: [
          { label: 'List accessible events', route: '/api/events' },
          { label: 'Authentication guide', route: '/authentication' },
        ],
      },
      {
        title: 'The complete journey',
        body: 'Read this diagram from top to bottom. Values shown in bold concepts—Selection URL, Reservation ID, Reservation token, and Checkout attempt ID—must be passed to the next stage exactly as returned.',
        flow: [
          {
            label: '1 · Your server',
            title: 'Create a Selection session',
            body: 'Send event_id, your buyer_session_ref, and the registered return_url. Save the selection_session_id for support and correlation.',
            route: '/api/selection-sessions/create',
          },
          {
            label: '2 · Your browser',
            title: 'Open the Selection URL',
            body: 'Redirect or link the buyer to selection_url. Do not copy, parse, log, or persist its fragment capability.',
          },
          {
            label: '3 · TktSync Selector',
            title: 'Buyer chooses and holds tickets',
            body: 'TktSync creates an authoritative Reservation. A successful hold is the first point at which inventory is actually owned.',
          },
          {
            label: '4 · Browser handoff',
            title: 'Receive the secure form POST',
            body: 'Continue to checkout submits reservation_id and reservation_token to your return_url as application/x-www-form-urlencoded fields.',
          },
          {
            label: '5 · Your server',
            title: 'Begin checkout',
            body: 'Put the token in X-TktSync-Reservation-Token. The response contains checkout_attempt.id; save it before contacting your payment provider.',
            route: '/api/reservations/begin-checkout',
          },
          {
            label: '6 · Your payment system',
            title: 'Collect payment',
            body: 'TktSync does not charge the buyer. Your application owns payment and decides whether the outcome is successful, definitively failed, or still unknown.',
          },
          {
            label: '7 · Your server',
            title: 'Confirm the Reservation',
            body: 'On known payment success, send checkout_attempt_id plus your order and payment references. Save every returned Ticket ID.',
            route: '/api/reservations/confirm',
          },
          {
            label: '8 · Your ticket experience',
            title: 'Retrieve and deliver each ticket',
            body: 'Retrieve the current credential and hosted QR for every Ticket ID, then place that QR inside your own branded ticket.',
            route: '/workflows/tickets',
          },
        ],
      },
      {
        title: 'Exactly what your return endpoint receives',
        body: 'Your endpoint must parse a normal HTML form body. These names are fixed. Respond with your checkout HTML or redirect only after your server has securely associated both values with the buyer’s checkout session.',
        code: {
          label: 'application/x-www-form-urlencoded POST body',
          value: 'reservation_id=res_...&reservation_token=<opaque-secret>',
        },
        callout:
          'Do not send the token to analytics, error trackers, browser storage, URLs, or routine logs. If you discard it, TktSync cannot recover the original plaintext token from the database. The buyer must create a new hold.',
      },
      {
        title: 'A minimal return-handler shape',
        body: 'The exact framework does not matter. Validate both fields, bind them to the authenticated or signed buyer checkout session, keep the token server-side, and show the buyer a payment page. The example is deliberately pseudocode so it does not imply one required web framework.',
        code: {
          label: 'Server-side pseudocode',
          value:
            'POST /tktsync/return\n  reservationId = form["reservation_id"]\n  reservationToken = form["reservation_token"]\n  require reservationId and reservationToken\n  checkoutStore.saveSecurely(buyerSession, reservationId, reservationToken)\n  renderPaymentPage(reservationId)',
        },
      },
      {
        title: 'Known failure versus unknown payment',
        body: 'If payment is definitively declined, report payment failure with the Checkout attempt ID. If the payment provider timed out or the result is uncertain, do not claim failure and do not start a different checkout blindly. Retrieve the Reservation and reconcile the provider outcome first.',
        links: [
          {
            label: 'Report definitive payment failure',
            route: '/api/reservations/payment-failure',
          },
          {
            label: 'Retrieve authoritative Reservation state',
            route: '/api/reservations/retrieve',
          },
          { label: 'Recovery workflow', route: '/workflows/recovery' },
        ],
      },
    ],
  },
  {
    route: '/workflows/direct',
    title: 'Build your own selection UI',
    eyebrow: 'Workflow B · Direct Reservations',
    summary:
      'Render the Event and inventory in your product, then ask TktSync to atomically reserve the buyer’s final choices.',
    sections: [
      {
        title: 'When to use this workflow',
        body: 'Choose this path only when your product wants to own the seat-selection interface. You may render sections, rows, tables, seats, and general-admission quantity controls, but you may not treat an availability response as ownership. Availability can change between display and submission.',
      },
      {
        title: 'The complete journey',
        body: 'Your server reads the Event model and availability, your browser renders it, and your server submits the buyer’s final offers as one atomic Reservation.',
        flow: [
          {
            label: '1 · Your server',
            title: 'Discover accessible Events',
            body: 'List Events dynamically instead of requiring anyone to paste opaque Event IDs into your application.',
            route: '/api/events',
          },
          {
            label: '2 · Your server',
            title: 'Read layout and availability',
            body: 'Use layout for structure and labels; use availability for current offers, prices, states, and GA bounds.',
            route: '/api/events/layout',
          },
          {
            label: '3 · Your UI',
            title: 'Collect customer intent',
            body: 'Present available options. Keep offer identifiers opaque and never invent seat or GA ownership locally.',
          },
          {
            label: '4 · Your server',
            title: 'Create one atomic Reservation',
            body: 'Submit every selected offer together. Either the complete request succeeds or none of it is acquired.',
            route: '/api/reservations/create',
          },
          {
            label: '5 · Your server',
            title: 'Save both returned values',
            body: 'Persist reservation.id and reservation_token securely. The token authorizes owner mutations and is returned only at creation.',
          },
          {
            label: '6 · Checkout and ticketing',
            title: 'Use the same commercial flow',
            body: 'Begin checkout, take payment, confirm, and deliver tickets exactly as in the Selector workflow.',
            route: '/api/reservations/begin-checkout',
          },
        ],
      },
      {
        title: 'Reserved seats and general admission differ',
        body: 'A reserved offer identifies one concrete inventory unit and always has quantity 1. A GA offer identifies a pool and accepts a bounded quantity. Use the identifiers and limits returned by TktSync; do not infer inventory identity from display labels.',
        callout:
          'If Reservation creation returns a conflict, refresh availability and ask the buyer to choose again. Never silently substitute a different seat.',
        links: [
          { label: 'Retrieve availability', route: '/api/events/availability' },
          { label: 'Create a Reservation', route: '/api/reservations/create' },
          { label: 'Modify a Reservation', route: '/api/reservations/update' },
        ],
      },
    ],
  },
  {
    route: '/workflows/tickets',
    title: 'Deliver a visible ticket',
    eyebrow: 'Workflow C · Ticket presentation',
    summary:
      'Turn the Ticket IDs returned by confirmation into a branded buyer experience with a valid TktSync QR.',
    sections: [
      {
        title: 'What confirmation gives you',
        body: 'A successful confirmation response contains one Sale and a tickets array. Save each public Ticket ID (tkt_...), status, and credential ID. Confirmation intentionally does not include the raw QR payload in the general response.',
        links: [{ label: 'Confirm a Reservation', route: '/api/reservations/confirm' }],
      },
      {
        title: 'Build one buyer ticket per Ticket ID',
        body: 'For every Ticket ID, retrieve its current credential. The response contains qr_payload for systems that render QR codes themselves and qr_url for systems that prefer a TktSync-hosted SVG image.',
        flow: [
          {
            label: '1 · Partner server',
            title: 'Retrieve the ticket credential',
            body: 'Authenticate with your Partner credential and request /tickets/{ticket_id}/credential.',
            route: '/api/tickets/retrieve',
          },
          {
            label: '2 · Choose presentation',
            title: 'Use qr_url or render qr_payload',
            body: 'Embed qr_url as the QR image, or encode the complete qr1 payload without changing a single character.',
          },
          {
            label: '3 · Partner-owned design',
            title: 'Add buyer-facing details',
            body: 'Show event name, date, venue, section or area, row/table/seat when applicable, buyer details, and your support information.',
          },
          {
            label: '4 · Deliver securely',
            title: 'Email, app, wallet, or PDF',
            body: 'Deliver the finished ticket through your product. Avoid analytics and caching that expose the QR capability.',
          },
        ],
      },
      {
        title: 'The hosted QR is not a complete ticket page',
        body: 'Opening qr_url returns only the QR image. TktSync does not add your logo, buyer name, terms, or support experience. This boundary lets the credential remain authoritative while your product controls visual design and delivery.',
        callout:
          'Do not create a QR code whose contents are the qr_url itself. Display the image returned by qr_url, or create a QR whose contents are the qr_payload value. TktSync ticket validation expects the embedded qr1 credential.',
        links: [
          {
            label: 'Retrieve credential and hosted URL',
            route: '/api/tickets/retrieve',
          },
          { label: 'Retrieve authenticated QR image', route: '/api/tickets/retrieve-qr' },
        ],
      },
      {
        title: 'Reissue and void are different',
        body: 'Reissue when the credential or hosted QR URL may have been exposed but the Ticket should remain valid. The old credential and old hosted presentation URL become invalid; retrieve the replacement pair for customer delivery. Void when the Ticket itself must no longer admit anyone. Voiding does not automatically make the sold inventory available again.',
        links: [
          { label: 'Reissue a credential', route: '/api/tickets/reissue-credential' },
          { label: 'Void a Ticket', route: '/api/tickets/void' },
          { label: 'Re-release eligible inventory', route: '/api/tickets/re-release-inventory' },
        ],
      },
    ],
  },
  {
    route: '/workflows/recovery',
    title: 'Retries and recovery',
    eyebrow: 'Workflow D · Safe failure handling',
    summary:
      'Recover from timeouts without charging twice, losing inventory, or claiming a payment result you do not actually know.',
    sections: [
      {
        title: 'The central rule',
        body: 'A timeout tells you that your application did not receive a response. It does not tell you whether the server completed the operation. For the same customer intent, retry the same operation with the same Idempotency-Key and unchanged request. Do not generate a fresh key merely because the network failed.',
      },
      {
        title: 'Recovery decision guide',
        body: 'Follow the branch that matches what you know, not what you hope happened.',
        flow: [
          {
            label: 'Request timed out',
            title: 'Retry identical intent',
            body: 'Use the same idempotency key and body. TktSync returns the original logical result or safely continues it.',
          },
          {
            label: 'Reservation state unclear',
            title: 'Read authoritative state',
            body: 'GET the Reservation. Its state and deadlines determine the next valid action.',
            route: '/api/reservations/retrieve',
          },
          {
            label: 'Payment definitely succeeded',
            title: 'Confirm',
            body: 'Use the exact Checkout attempt ID returned by Begin checkout and include your provider references.',
            route: '/api/reservations/confirm',
          },
          {
            label: 'Payment definitely failed',
            title: 'Report failure',
            body: 'Report failure only when the provider gives a definitive decline or failure outcome.',
            route: '/api/reservations/payment-failure',
          },
          {
            label: 'Payment outcome unknown',
            title: 'Reconcile first',
            body: 'Do not report failure. Query the payment provider and Reservation state until the outcome is known or follow your formal reconciliation process.',
          },
        ],
      },
      {
        title: 'Useful state meanings',
        body: 'HELD means inventory is reserved but checkout has not started. COMMITTING means an active Checkout attempt protects the transaction. PAYMENT_RETRY means a definitive failure was reported and another permitted attempt may be made before its deadline. RECONCILING means the outcome needs resolution. CONFIRMED, RELEASED, and EXPIRED are terminal for the Reservation.',
      },
      {
        title: 'Values to keep for support and reconciliation',
        body: 'Store your buyer session reference, Reservation ID, Checkout attempt ID, idempotency keys, partner order reference, partner payment reference, and TktSync request IDs. Store Reservation tokens only in an appropriately protected secret field. Never store Partner credentials or Reservation tokens in client-visible logs.',
        links: [
          { label: 'Idempotency guide', route: '/idempotency' },
          { label: 'Error guide', route: '/errors' },
        ],
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
        body: 'Send Authorization: Bearer <partner-credential> over HTTPS. Credentials identify the Partner, not an end user. Request rotation or revocation through your Partner integration support channel.',
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
        body: 'TktSync delivers subscribed outbox events at least once to the Partner HTTPS endpoints registered during integration setup. Deduplicate by the stable event id. Any 2xx response acknowledges delivery; redirects and every non-2xx response do not.',
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
        body: 'Webhook endpoint registration, subscribed event types, secret rotation, and endpoint disablement are agreed through Partner integration setup. They are not runtime calls made by your customer-facing application.',
      },
    ],
  },
];
