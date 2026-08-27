/* eslint-disable react-refresh/only-export-components */
import { getReviewConfig } from './config';
import ReviewPlayer from './ReviewPlayer';

const config = getReviewConfig();

function TktSyncLogo() {
  return (
    <span className="tkt-logo" aria-hidden="true">
      <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
        <rect x="1" y="6" width="30" height="20" rx="5" className="tkt-logo-fill" />
        <path
          d="M12 6v3.2M12 14.2v3.6M12 22.8V26"
          className="tkt-logo-stroke"
          strokeWidth="1.6"
          strokeLinecap="round"
          opacity=".85"
        />
        <path
          d="M25 14.4a5.2 5.2 0 0 0-9.4-1.7"
          className="tkt-logo-stroke"
          strokeWidth="1.9"
          strokeLinecap="round"
        />
        <path
          d="M15.6 17.6a5.2 5.2 0 0 0 9.4 1.7"
          className="tkt-logo-stroke"
          strokeWidth="1.9"
          strokeLinecap="round"
          opacity=".6"
        />
        <path
          d="M25.4 11.4v3.2h-3.2"
          className="tkt-logo-stroke"
          strokeWidth="1.9"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M15.2 20.6v-3.2h3.2"
          className="tkt-logo-stroke"
          strokeWidth="1.9"
          strokeLinecap="round"
          strokeLinejoin="round"
          opacity=".6"
        />
      </svg>
      <span className="tkt-logo-word">
        Tkt<span>Sync</span>
      </span>
    </span>
  );
}

type GuideAction = {
  title: string;
  location: string;
  instructions: string[];
  complete: string;
  note?: string;
  href?: string;
  cta?: string;
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

export const phases: ReviewPhase[] = [
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
        title: 'Review the Dashboard',
        location: 'Dashboard',
        instructions: [
          'Sign in with the Reviewer access shown at the start of this phase. Use the email exactly as displayed.',
          'Read the four headline metrics: Active events, Tickets sold, Reservations today and Check-ins today.',
          'Scan Upcoming events, Attention needed and Recent activity. These are summaries—not the setup order.',
        ],
        complete: 'Dashboard metrics and setup summaries have been reviewed. Continue to Venues.',
        note: 'Do not start with “Create event” for a brand-new setup. An Event needs a venue with a published layout first.',
      },
      {
        title: 'Create the venue',
        location: 'Venues → Add venue',
        href: appPath(config.admin, '/venues'),
        cta: 'Open Venues',
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
        href: appPath(config.admin, '/venues'),
        cta: 'Open Venues',
        instructions: [
          'Choose New layout version, then Edit draft.',
          'Add at least one Reserved section and set its row and seat counts.',
          'Add a General admission area and set its standing capacity.',
          'Optionally add a Table area, then add Stage, Ring or Field orientation to the layout.',
        ],
        complete:
          'The builder totals match the sections, reserved seats and GA capacity you intended.',
      },
      {
        title: 'Save, preview and publish the layout',
        location: 'Visual floor-plan builder → Venue detail',
        href: appPath(config.admin, '/venues'),
        cta: 'Open Venues',
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
        href: appPath(config.admin, '/events/new'),
        cta: 'Open Create Event',
        instructions: [
          'Use the review Event name shown in this guide, then select the venue you just prepared.',
          'Set Event start to a future date and Event end to a later time on that date.',
          'Set Sales open to at least 10 minutes before the current time. This is essential: do not use a future Sales open time.',
          'Set Sales close after the current time, then set Admission open before the Event starts and Admission close after admission opens.',
          'Confirm that all six date and time fields are filled and the timezone is correct.',
          'On Review, verify every schedule value. If any line says Not scheduled, choose Back and enter it again before creating the Event.',
          'Review the summary and choose Create draft event.',
        ],
        complete: 'The Event opens in Draft state with an Event setup checklist.',
        note: 'Sales open must be in the past so Step 14 presents Open sales. Creating the draft still does not put tickets on sale until you deliberately choose that action.',
      },
      {
        title: 'Materialize the published layout',
        location: 'Event → Layout & seats',
        href: appPath(config.admin, '/events'),
        cta: 'Open Events',
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
        href: appPath(config.admin, '/events'),
        cta: 'Open Events',
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
        href: appPath(config.admin, '/events'),
        cta: 'Open Events',
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
        href: appPath(config.admin, '/events'),
        cta: 'Open Events',
        instructions: [
          'Reconcile Available, Held, Sold and Blocked totals with the materialized layout.',
          'Confirm the inventory table includes both reserved units and the GA pool.',
          'Open Block inventory and Create allocation to review the available controls, then cancel without saving unless you deliberately need them.',
        ],
        complete:
          'All sellable units are priced and available; no accidental block or allocation restricts the demo.',
        note: 'Blocks and allocations are optional operational controls. They are not prerequisites for opening sales.',
      },
      {
        title: 'Create the Partner and save its credential',
        location: 'Partners → Add partner → Partner detail',
        href: appPath(config.admin, '/partners'),
        cta: 'Open Partners',
        instructions: [
          'Open Partners, choose Add partner, enter the storefront Partner name and create it.',
          `On the new Partner Overview, enter ${appPath(config.partner, '/checkout/return')} under Checkout return URLs and choose Save checkout URLs. Wait for Saved to appear.`,
          'Open the new Partner and choose Issue credential.',
          'Copy the complete credential immediately and save it somewhere temporary and secure before closing the dialog.',
          'Do not choose I have stored it until you have saved the credential. TktSync will not display it again.',
        ],
        complete:
          'The Partner is Active, the exact Demo Partner checkout-return URL is saved, and the complete one-time credential is safely available for the Demo Partner setup step.',
        note: `The required return URL is ${appPath(config.partner, '/checkout/return')}. You will paste the saved credential into Demo Partner setup. If it is lost, you must issue a new credential.`,
      },
      {
        title: 'Grant your Partner Event access',
        location: 'Event → Partners',
        href: appPath(config.admin, '/events'),
        cta: 'Open Events',
        instructions: [
          'Find the Partner you just created in the table.',
          'Confirm the Partner itself is Active, then choose Grant access.',
          'Wait for Event access to change from No access to Enabled.',
        ],
        complete:
          'Your Partner shows Enabled and the Event can be discovered through its saved Partner API credential.',
      },
      {
        title: 'Run the readiness check and open sales',
        location: 'Event → Overview',
        href: appPath(config.admin, '/events'),
        cta: 'Open Events',
        instructions: [
          'Confirm Sales policy, Layout & seats, Pricing and Inventory all show Configured.',
          'Check that Partner access is 1 or more.',
          'Confirm the primary action says Open sales. If it says Schedule sales, the Sales open time was set in the future: do not continue to the Partner storefront; create a replacement Event with Sales open at least 10 minutes in the past and add a short unique suffix such as “Retry 2” to its name.',
          'Choose Open sales only after those checks pass.',
          'Wait for the Event badge to say On sale before leaving Admin.',
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
      'The connected Partner owns this storefront, buyer journey and branding. TktSync supplies the Event, inventory and ticketing infrastructure underneath it.',
    action: 'Open Demo Partner',
    href: config.partner,
    demo: true,
    actions: [
      {
        title: 'Choose the Partner storefront',
        location: 'Demo Partner setup',
        href: config.partner,
        cta: 'Open Partner setup',
        instructions: [
          'Use the exact Partner name and the credential you saved earlier in Admin, then choose Connect and view Events.',
          'The name labels this sample storefront; the credential is what securely identifies the Partner to the TktSync API.',
          'To reuse a previous connection, choose View saved Partners and then Use this Partner.',
          'Confirm the storefront header now shows the selected Partner name.',
        ],
        complete: 'The selected Partner name appears above its live Event catalogue.',
        note: 'The credential is the one-time value you saved when it was issued. If the page reports a deployment configuration problem, do not replace the credential—the Demo Partner server must be connected to the deployed API first.',
      },
      {
        title: 'Confirm the application boundary',
        location: 'Connected Partner demo storefront',
        href: appPath(config.partner, '/events'),
        cta: 'Open Partner Events',
        instructions: [
          'Read the Demo Partner Application notice before using the storefront.',
          'Notice that this screen looks intentionally different from TktSync Admin, Selector and Scanner.',
          'Confirm the Event list contains only live Events returned by the real Partner API.',
        ],
        complete:
          'The storefront is identified as a sample external application, not a TktSync Partner portal.',
      },
      {
        title: 'Open the Event you created',
        location: 'Events → your review Event → View event',
        href: appPath(config.partner, '/events'),
        cta: 'Open Partner Events',
        instructions: [
          'Check the Event name, date, venue, sale state and starting price.',
          'Expand Assessment note · How this works.',
          'Choose tickets.',
        ],
        complete:
          'The browser leaves the Partner storefront and opens a TktSync-hosted Selector session for this Event.',
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
        title: 'Review the seat map',
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
        complete: 'The Partner storefront displays Review your order with a live hold countdown.',
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
        location: 'Partner storefront → Review your order',
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
        location: 'Partner storefront → Demo payment',
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
        location: 'Partner storefront → Your ticket',
        instructions: [
          'Verify Event, venue, date, section, row and seat or area.',
          'Find the Event reference, public Ticket ID and Active status. Keep the Event reference visible for Scanner.',
          'Confirm the QR image is hosted by TktSync even though the surrounding ticket design belongs to the Partner.',
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
          'Choose the Event whose Event reference matches the reference printed on the issued ticket. This distinguishes Events even when their names, dates and venues match.',
          'On a phone, allow rear-camera access only when prompted. On desktop, use the supported manual credential entry when available.',
        ],
        complete: 'Scanner shows the selected Event and is ready to validate a credential.',
      },
      {
        title: 'Prove admission and duplicate protection',
        location: 'Scanner → scan',
        instructions: [
          'Scan the QR from the Partner ticket.',
          'Read the ADMITTED result and verify the expected ticket/seat details.',
          'Scan the same QR again.',
        ],
        complete: 'The first scan is admitted and the second is rejected as Already admitted.',
        note: 'The second scan must return Already admitted. A second successful admission would be incorrect.',
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
          'Open the Event you created, choose Sales and find the newly confirmed sale.',
          'Filter by that Event or search for the public Ticket reference.',
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

export function LegacyWalkthrough() {
  const totalActions = phases.reduce((total, phase) => total + phase.actions.length, 0);

  return (
    <>
      <header className="top">
        <a className="brand" href="#top" aria-label="TktSync Review Guide home">
          <TktSyncLogo />
          <strong>Review Guide</strong>
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
            Complete the phases in order. Each action identifies the screen, the required steps and
            the result to verify before continuing.
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
            <p className="eyebrow">Product boundaries</p>
            <h2 id="boundary-title">One infrastructure product. One sample storefront.</h2>
          </div>
          <div className="boundary-grid">
            <article>
              <p className="surface-label">TktSync product surfaces</p>
              <p>Admin Console · Selector · Scanner · Partner API · Developer Docs</p>
            </article>
            <article className="demo-boundary">
              <p className="surface-label demo">Demo-only surface</p>
              <h3>Connected Partner storefront</h3>
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
                            <strong>Check</strong>
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
                    <h3>Review the implementation</h3>
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

export default ReviewPlayer;
