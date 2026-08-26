import { getReviewConfig } from './config';

const config = getReviewConfig();

type GuideAction = {
  title: string;
  location: string;
  instructions: string[];
  complete: string;
  note?: string;
};

type ReviewPhase = {
  id: string;
  name: string;
  shortName: string;
  surface: string;
  purpose: string;
  action: string;
  href: string;
  actions: GuideAction[];
  demo?: boolean;
  access?: boolean;
};

function appPath(base: string, path: string) {
  if (!base) return '';
  try {
    return new URL(path, `${base.replace(/\/$/, '')}/`).toString();
  } catch {
    return base;
  }
}

const phases: ReviewPhase[] = [
  {
    id: 'admin-setup',
    name: 'Configure an Event in Admin',
    shortName: 'Admin setup',
    surface: 'TktSync product surface',
    purpose:
      'Build the supply side in its real dependency order: venue first, then a published layout, then the Event, pricing, inventory and Partner access.',
    action: 'Open Admin Console',
    href: config.admin,
    access: true,
    actions: [
      {
        title: 'Orient yourself on the Dashboard',
        location: 'Dashboard',
        instructions: [
          'Sign in with the Reviewer access shown at the start of this phase. Use the email exactly as displayed.',
          'Read the four headline metrics: Active events, Tickets sold, Reservations today and Check-ins today.',
          'Scan Upcoming events, Attention needed and Recent activity. These are summaries—not the setup order.',
        ],
        complete:
          'You understand what the Dashboard reports and are ready to create the venue prerequisite.',
        note: 'Do not start with “Create event” for a brand-new setup. An Event needs a venue with a published layout first.',
      },
      {
        title: 'Create the venue',
        location: 'Venues → Add venue',
        instructions: [
          'Open Venues from the left navigation, then choose Add venue.',
          'Enter a recognizable venue name and address, then create it.',
          'Open the new venue. Its Layout versions count begins at zero.',
        ],
        complete: 'The venue detail page is visible and is ready for its first layout version.',
      },
      {
        title: 'Create a layout draft',
        location: 'Venue → New layout version',
        instructions: [
          'Choose New layout version, then Edit draft.',
          'Add at least one Reserved section and set its row and seat counts.',
          'Add a General admission area and set its standing capacity.',
          'Optionally add a Table area, then add Stage, Ring or Field orientation so the buyer can understand the plan.',
        ],
        complete:
          'The builder totals match the sections, reserved seats and GA capacity you intended.',
      },
      {
        title: 'Save, preview and publish the layout',
        location: 'Visual floor-plan builder → Venue detail',
        instructions: [
          'Save the draft, then open Preview buyer view and verify labels and orientation.',
          'Return to the venue and choose Publish.',
          'Read the warning, then confirm Publish layout.',
        ],
        complete: 'Published shows 1 and Drafts shows 0. The version is now available to Events.',
        note: 'Publishing is irreversible. Later venue edits do not silently change Events already using this version.',
      },
      {
        title: 'Create the Event draft',
        location: 'Events → Create event',
        instructions: [
          'Enter the Event name and select the venue you just prepared.',
          'Set start/end, sales and admission windows when required, and confirm the timezone.',
          'Review the summary and choose Create draft event.',
        ],
        complete: 'The Event opens in Draft state with an Event setup checklist.',
        note: 'Creating the draft does not put tickets on sale. Finish every setup item before opening sales.',
      },
      {
        title: 'Materialize the published layout',
        location: 'Event → Layout & seats',
        instructions: [
          'Choose the published venue layout version.',
          'Select Materialize layout.',
          'Read the success message: authoritative inventory has now been created for this Event.',
        ],
        complete:
          'Layout & seats says the layout is materialized and the Overview capacity is no longer zero.',
      },
      {
        title: 'Create price tiers',
        location: 'Event → Pricing → Add price tier',
        instructions: [
          'Add a customer-facing name, short code, currency and price for each tier.',
          'Create separate tiers for the areas you want to price differently—for example VIP, Table and GA.',
          'After each tier is created, check the displayed amount before continuing.',
        ],
        complete:
          'Every intended price tier appears as Active with the correct displayed currency amount.',
        note: 'Use the visible result as your check: entering 25000 should display ₦25,000. Correct an unexpected amount before assigning it.',
      },
      {
        title: 'Assign pricing to inventory',
        location: 'Event → Pricing → Assign pricing',
        instructions: [
          'Choose a price tier, then choose whether it applies to the entire Event, one area, or specific seats.',
          'For area pricing, select the matching section and choose Review pricing assignment.',
          'Confirm Apply pricing, then repeat until Reserved, Table and GA inventory all have prices.',
        ],
        complete:
          'Event Overview marks Pricing as Configured and Inventory shows Assigned for every sellable unit.',
      },
      {
        title: 'Review inventory controls',
        location: 'Event → Inventory',
        instructions: [
          'Reconcile Available, Held, Sold and Blocked totals with the materialized layout.',
          'Confirm the inventory table includes both reserved units and the GA pool.',
          'Open Block inventory and Create allocation to understand the controls, then cancel without saving unless you deliberately need them.',
        ],
        complete:
          'All sellable units are priced and available; no accidental block or allocation restricts the demo.',
        note: 'Blocks and allocations are optional operational controls. They are not prerequisites for opening sales.',
      },
      {
        title: 'Grant the Demo Partner Event access',
        location: 'Event → Partners',
        instructions: [
          'Find Demo Partner in the table.',
          'Confirm the Partner itself is Active, then choose Grant access.',
          'Wait for Event access to change from No access to Enabled.',
        ],
        complete:
          'Demo Partner shows Enabled and the Event can be discovered through its Partner API credential.',
      },
      {
        title: 'Run the readiness check and open sales',
        location: 'Event → Overview',
        instructions: [
          'Confirm Sales policy, Layout & seats, Pricing and Inventory all show Configured.',
          'Check that Partner access is 1 or more.',
          'Choose Open sales only after those checks pass.',
        ],
        complete:
          'The Event status changes from Draft to On sale and Pause sales replaces Open sales.',
      },
    ],
  },
  {
    id: 'partner',
    name: 'Enter the Demo Partner Storefront',
    shortName: 'Partner',
    surface: 'Demo-only application',
    purpose:
      'See what an independent ticketing company builds around TktSync. Northstar owns this storefront and its branding; TktSync remains underneath it.',
    action: 'Open Demo Partner',
    href: config.partner,
    demo: true,
    actions: [
      {
        title: 'Confirm the application boundary',
        location: 'Northstar Tickets — Demo',
        instructions: [
          'Read the Demo Partner Application notice before using the storefront.',
          'Notice that this screen looks intentionally different from TktSync Admin, Selector and Scanner.',
          'Confirm the Event list contains only live Events returned by the real Partner API.',
        ],
        complete:
          'It is clear that Northstar is a sample external storefront, not a TktSync Partner portal.',
      },
      {
        title: 'Open the prepared Event',
        location: 'Events → Championship Night → View event',
        instructions: [
          'Check the Event name, date, venue, sale state and starting price.',
          'Expand Assessment note · How this works.',
          'Choose tickets.',
        ],
        complete:
          'The browser leaves Northstar and opens a TktSync-hosted Selector session for this Event.',
        note: 'The Partner creates the Selection session on its server. Its credential is never sent to browser JavaScript.',
      },
    ],
  },
  {
    id: 'selector',
    name: 'Select and Hold Real Inventory',
    shortName: 'Selector',
    surface: 'TktSync product surface',
    purpose:
      'Use the buyer-facing Selector to choose inventory while TktSync performs the authoritative availability check and hold.',
    action: 'Start from Demo Partner',
    href: config.partner,
    actions: [
      {
        title: 'Understand the seat map',
        location: 'TktSync Selector',
        instructions: [
          'Confirm the same sections and orientation created in the venue layout are visible here.',
          'Compare available and unavailable states and inspect the price before selecting.',
          'Choose one reserved seat, table seat or GA quantity.',
        ],
        complete: 'The selection summary names the inventory and shows its authoritative price.',
      },
      {
        title: 'Create the hold and return to checkout',
        location: 'Selector → selection summary',
        instructions: [
          'Review the selected ticket and total.',
          'Continue to reserve the inventory.',
          'Wait for the secure return to the Partner checkout; do not copy or edit the return address.',
        ],
        complete: 'Northstar displays Review your order with a live hold countdown.',
        note: 'The Reservation token is posted directly to the Partner server. It must not appear in the URL, browser storage or logs.',
      },
    ],
  },
  {
    id: 'checkout',
    name: 'Complete Checkout and Inspect the Ticket',
    shortName: 'Ticket',
    surface: 'Partner presentation + TktSync authority',
    purpose:
      'Follow the commercial lifecycle without a real payment provider: the Partner owns payment; TktSync owns the Reservation, ticket identity and QR credential.',
    action: 'Continue from Demo Partner',
    href: config.partner,
    actions: [
      {
        title: 'Review the held order',
        location: 'Northstar → Review your order',
        instructions: [
          'Check Event, selected tickets, quantities, unit prices, total and hold countdown.',
          'Confirm the browser URL contains only the checkout identifier—not a Reservation token.',
          'Choose Continue to payment before the hold expires.',
        ],
        complete:
          'The explicit Demo payment screen appears and explains that payment is simulated by the Partner.',
      },
      {
        title: 'Simulate payment and confirm the Reservation',
        location: 'Northstar → Demo payment',
        instructions: [
          'Choose Simulate successful payment.',
          'Wait while the Partner begins checkout and then confirms the real Reservation lifecycle.',
          'Do not refresh while confirmation is in progress.',
        ],
        complete:
          'A completed ticket page appears. No ticket was created directly or before Reservation confirmation.',
      },
      {
        title: 'Inspect the issued ticket and hosted QR',
        location: 'Northstar → Your ticket',
        instructions: [
          'Verify Event, venue, date, section, row and seat or area.',
          'Find the public Ticket ID and Active status.',
          'Confirm the QR image is hosted by TktSync even though the surrounding ticket design belongs to Northstar.',
        ],
        complete:
          'You can distinguish Partner-owned presentation from the TktSync-owned ticket and admission credential.',
      },
    ],
  },
  {
    id: 'scanner',
    name: 'Validate Admission in Scanner',
    shortName: 'Scanner',
    surface: 'TktSync product surface',
    purpose:
      'Use the separate gate application to prove that admission is checked against the same authoritative ticket state.',
    action: 'Open Scanner',
    href: config.scanner,
    access: true,
    actions: [
      {
        title: 'Prepare the gate',
        location: 'Scanner → sign in',
        instructions: [
          'Sign in with the Reviewer access shown at the start of this phase. Use the email exactly as displayed.',
          'Choose the same Event used for the purchase.',
          'On a phone, allow rear-camera access only when prompted. On desktop, use the supported manual credential entry when available.',
        ],
        complete: 'Scanner shows the selected Event and is ready to validate a credential.',
      },
      {
        title: 'Prove admission and duplicate protection',
        location: 'Scanner → scan',
        instructions: [
          'Scan the QR from the Northstar ticket.',
          'Read the ADMITTED result and verify the expected ticket/seat details.',
          'Scan the same QR again.',
        ],
        complete: 'The first scan is admitted and the second is rejected as Already admitted.',
        note: 'Do not expect a second green result. Duplicate rejection is the security behavior being demonstrated.',
      },
    ],
  },
  {
    id: 'admin-proof',
    name: 'Return to Admin and Verify the Outcome',
    shortName: 'Admin proof',
    surface: 'TktSync product surface',
    purpose:
      'Close the loop by finding the buyer journey in the operational records and authoritative reports.',
    action: 'Return to Admin Console',
    href: appPath(config.admin, '/tickets'),
    access: true,
    actions: [
      {
        title: 'Find the issued Ticket',
        location: 'Admin → Event → Sales, then Tickets',
        instructions: [
          'Open the prepared Event, choose Sales and find the newly confirmed sale.',
          'Filter by the prepared Event or search for the public Ticket reference.',
          'Verify the Ticket is Active and the seat/area matches the buyer selection.',
          'Confirm Entry now shows Admitted.',
        ],
        complete: 'The Partner-presented ticket is visible as an authoritative TktSync record.',
      },
      {
        title: 'Inspect admission history',
        location: 'Admin → Admissions',
        instructions: [
          'Select the same Event.',
          'Find the Admitted record for the first scan.',
          'Find the separate Already admitted outcome created by the duplicate scan.',
        ],
        complete: 'Both the successful admission and duplicate attempt are auditable.',
      },
      {
        title: 'Reconcile the reports and activity',
        location: 'Admin → Reports, then Dashboard',
        instructions: [
          'Select the Event in Reports and compare Tickets sold, Revenue, Available and Admitted.',
          'Check the Inventory position, Commercial performance and Admission outcomes sections.',
          'Return to Dashboard and find Reservation checkout begun, Reservation confirmed and Ticket admitted in Recent activity.',
        ],
        complete: 'Commercial, inventory and gate outcomes all reconcile to one completed journey.',
      },
    ],
  },
];

function AccessDetails() {
  const hasCredentials = Boolean(config.accessEmail || config.accessPassword);
  if (!hasCredentials && !config.accessLabel && !config.accessInstructions) return null;

  return (
    <aside className="access-card" aria-label="Reviewer access">
      <div>
        <p className="access-title">Reviewer access</p>
        {config.accessLabel && <p>{config.accessLabel}</p>}
        {config.accessInstructions && <p>{config.accessInstructions}</p>}
      </div>
      {hasCredentials && (
        <dl>
          {config.accessEmail && (
            <div>
              <dt>Email</dt>
              <dd>
                <code>{config.accessEmail}</code>
              </dd>
            </div>
          )}
          {config.accessPassword && (
            <div>
              <dt>Password</dt>
              <dd>
                <code>{config.accessPassword}</code>
              </dd>
            </div>
          )}
        </dl>
      )}
    </aside>
  );
}

function App() {
  const totalActions = phases.reduce((total, phase) => total + phase.actions.length, 0);

  return (
    <>
      <header className="top">
        <a className="brand" href="#top" aria-label="TktSync Review Guide home">
          <span aria-hidden="true">T</span>
          <strong>TktSync Review Guide</strong>
        </a>
        <a className="source-link" href={config.source} target="_blank" rel="noreferrer">
          Source code <span aria-hidden="true">↗</span>
        </a>
      </header>

      <main id="top">
        <section className="intro" aria-labelledby="intro-title">
          <p className="eyebrow">Assessment walkthrough</p>
          <h1 id="intro-title">Follow one ticket from setup to the gate.</h1>
          <p className="intro-copy">
            This guide assumes no ticketing or technical knowledge. Complete the phases in order;
            every action tells you where to go, what to do and how to know it worked.
          </p>
          <div className="hero-actions">
            <a className="hero-primary" href="#admin-setup">
              Begin with Admin Console
            </a>
            <a className="hero-secondary" href={config.source} target="_blank" rel="noreferrer">
              View source code ↗
            </a>
          </div>
          <div className="journey-summary" aria-label="Complete ticket journey">
            <p>
              <strong>6 guided phases</strong>
              <span>{totalActions} explicit actions</span>
            </p>
            <ol>
              <li>Venue + Event setup</li>
              <li>Partner storefront</li>
              <li>Ticket selection</li>
              <li>Checkout + Ticket</li>
              <li>Gate admission</li>
              <li>Admin proof</li>
            </ol>
          </div>
        </section>

        <section className="boundary" aria-labelledby="boundary-title">
          <div>
            <p className="eyebrow">Know what you are looking at</p>
            <h2 id="boundary-title">One infrastructure product. One sample storefront.</h2>
          </div>
          <div className="boundary-grid">
            <article>
              <p className="surface-label">TktSync product surfaces</p>
              <p>Admin Console · Selector · Scanner · Partner API · Developer Docs</p>
            </article>
            <article className="demo-boundary">
              <p className="surface-label demo">Demo-only surface</p>
              <h3>Northstar Tickets</h3>
              <p>
                This sample storefront is not part of TktSync. It only demonstrates how an
                independent ticketing platform uses the real Partner API.
              </p>
            </article>
          </div>
        </section>

        <nav className="phase-nav" aria-label="Guided review phases">
          <ol>
            {phases.map((phase, index) => (
              <li key={phase.id}>
                <a href={`#${phase.id}`}>
                  <span>{index + 1}</span>
                  {phase.shortName}
                </a>
              </li>
            ))}
            <li>
              <a href="#technical">
                <span>7</span>Technical
              </a>
            </li>
          </ol>
        </nav>

        <section className="walkthrough" aria-labelledby="walkthrough-title">
          <div className="walkthrough-heading">
            <p className="eyebrow">Full guided review · 10–15 minutes</p>
            <h2 id="walkthrough-title">Do these actions in order</h2>
            <p>Keep this guide open in one tab and open each application in another.</p>
          </div>
          <ol className="phases">
            {phases.map((phase, phaseIndex) => (
              <li id={phase.id} key={phase.id} className="phase">
                <div className="phase-number" aria-hidden="true">
                  {phaseIndex + 1}
                </div>
                <article className="phase-content">
                  <header className="phase-header">
                    <div>
                      <p className={phase.demo ? 'surface-label demo' : 'surface-label'}>
                        {phase.surface}
                      </p>
                      <h3>{phase.name}</h3>
                    </div>
                    {phase.demo && <span className="demo-badge">Not part of TktSync</span>}
                  </header>
                  <p className="phase-purpose">{phase.purpose}</p>
                  {phase.access && <AccessDetails />}
                  <a className="phase-cta" href={phase.href} target="_blank" rel="noreferrer">
                    {phase.action} <span aria-hidden="true">→</span>
                  </a>
                  <ol className="actions">
                    {phase.actions.map((action, actionIndex) => (
                      <li key={action.title} className="action-card">
                        <header>
                          <span>
                            {phaseIndex + 1}.{actionIndex + 1}
                          </span>
                          <div>
                            <p>{action.location}</p>
                            <h4>{action.title}</h4>
                          </div>
                        </header>
                        <ol className="action-instructions">
                          {action.instructions.map((instruction) => (
                            <li key={instruction}>{instruction}</li>
                          ))}
                        </ol>
                        {action.note && (
                          <aside className="action-note">
                            <strong>Important</strong>
                            <p>{action.note}</p>
                          </aside>
                        )}
                        <p className="done">
                          <span aria-hidden="true">✓</span>
                          <strong>Done when:</strong> {action.complete}
                        </p>
                      </li>
                    ))}
                  </ol>
                  {phaseIndex < phases.length - 1 && (
                    <a className="next-phase" href={`#${phases[phaseIndex + 1]!.id}`}>
                      Next: {phases[phaseIndex + 1]!.name} ↓
                    </a>
                  )}
                </article>
              </li>
            ))}
            <li id="technical" className="phase">
              <div className="phase-number" aria-hidden="true">
                7
              </div>
              <article className="phase-content">
                <header className="phase-header">
                  <div>
                    <p className="surface-label">Technical review</p>
                    <h3>Understand the implementation</h3>
                  </div>
                </header>
                <p className="phase-purpose">
                  After the visible journey is clear, inspect the contract and the controls that
                  make it safe.
                </p>
                <div className="technical-grid">
                  <a href={config.docs} target="_blank" rel="noreferrer">
                    <strong>Developer Docs</strong>
                    <span>Partner workflow and API reference →</span>
                  </a>
                  <a href={config.architecture} target="_blank" rel="noreferrer">
                    <strong>Architecture</strong>
                    <span>System structure and boundaries →</span>
                  </a>
                  <a href={config.security} target="_blank" rel="noreferrer">
                    <strong>Security model</strong>
                    <span>Credentials, tokens and fail-closed rules →</span>
                  </a>
                  <a href={config.runtime} target="_blank" rel="noreferrer">
                    <strong>Runtime model</strong>
                    <span>Concurrency and operational behavior →</span>
                  </a>
                  <a href={config.source} target="_blank" rel="noreferrer">
                    <strong>Source code</strong>
                    <span>github.com/Kahmyl/tktsync →</span>
                  </a>
                </div>
              </article>
            </li>
          </ol>
        </section>
      </main>

      <footer>
        <span>TktSync assessment review</span>
        <span>Setup → Partner → Selector → Ticket → Scanner → Admin proof</span>
      </footer>
    </>
  );
}

export default App;
