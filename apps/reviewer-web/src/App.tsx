import { getReviewConfig } from './config';

const config = getReviewConfig();

type ReviewStep = {
  id: string;
  name: string;
  surface: string;
  description: string;
  review: string[];
  verify: string[];
  action: string;
  href: string;
  demo?: boolean;
  access?: boolean;
};

const steps: ReviewStep[] = [
  {
    id: 'admin',
    name: 'Admin Console',
    surface: 'TktSync product surface',
    description:
      'The operational workspace used to configure venues, Events, inventory, pricing and Partner access.',
    review: [
      'Sign in with the configured reviewer access.',
      'Open Venues → Meridian Arena and inspect its published layout.',
      'Open Events → Championship Night.',
      'Review Overview, Layout & seats, Pricing, Inventory and Partners.',
      'Confirm the Event is on sale and the Demo Partner has access.',
    ],
    verify: [
      'The published venue layout has been materialized into reserved, table and general-admission inventory.',
      'Price tiers are assigned and availability is managed centrally by TktSync.',
    ],
    action: 'Open Admin Console',
    href: config.admin,
    access: true,
  },
  {
    id: 'partner',
    name: 'Demo Partner',
    surface: 'Demo-only application',
    description:
      'A sample external ticketing storefront. It is not part of TktSync; it demonstrates an independent Partner using the real TktSync Partner API.',
    review: [
      'Open the prepared Event in the storefront.',
      'Check the Event, venue, sale state and starting price.',
      'Choose tickets to begin the buyer journey.',
    ],
    verify: [
      'The storefront branding belongs to the Partner while its Event and ticket availability come from TktSync.',
      'Choosing tickets creates a real selection session before opening the TktSync Selector.',
    ],
    action: 'Open Demo Partner',
    href: config.partner,
    demo: true,
  },
  {
    id: 'selector',
    name: 'TktSync Selector',
    surface: 'TktSync product surface',
    description:
      'The TktSync-hosted buyer selection surface. Enter it from the Demo Partner so it receives a valid Partner-created selection session.',
    review: [
      'Continue from the prepared Event in the Demo Partner.',
      'Choose reserved seating or a general-admission quantity.',
      'Place the inventory on hold, then continue back to Partner checkout.',
    ],
    verify: [
      'Available and unavailable inventory are clearly distinguished.',
      'TktSync authoritatively checks and holds inventory; the Demo Partner does not maintain its own inventory truth.',
    ],
    action: 'Continue through Demo Partner',
    href: config.partner,
  },
  {
    id: 'checkout',
    name: 'Partner Checkout + Ticket',
    surface: 'Partner presentation + TktSync authority',
    description:
      'Checkout and the surrounding ticket design belong to the Demo Partner. Reservation confirmation, ticket identity and QR authority belong to TktSync.',
    review: [
      'Review the held tickets, prices, total and hold countdown.',
      'Continue to the explicit Demo payment step and simulate a successful payment.',
      'Inspect the issued ticket details and TktSync-hosted QR.',
    ],
    verify: [
      'The Partner begins checkout before confirming the Reservation through the real API lifecycle.',
      'The resulting ticket has an authoritative public ID, status and admission credential.',
    ],
    action: 'Continue in Demo Partner',
    href: config.partner,
  },
  {
    id: 'scanner',
    name: 'Scanner',
    surface: 'TktSync product surface',
    description:
      'The venue admission interface that checks every ticket against TktSync’s authoritative admission state.',
    review: [
      'Sign in with the configured reviewer access and select Championship Night.',
      'Scan the ticket QR from Step 4, or use the supported manual code entry.',
      'Submit the same credential a second time.',
    ],
    verify: [
      'The first valid scan is admitted.',
      'The distinct repeat scan is rejected as already checked in.',
    ],
    action: 'Open Scanner',
    href: config.scanner,
    access: true,
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
          <h1 id="intro-title">Review the complete TktSync workflow in order.</h1>
          <p>
            Each step explains what to inspect and verify before you open the corresponding
            application. Start with Event configuration, then follow one ticket through purchase and
            admission.
          </p>
          <nav aria-label="Review progression">
            <ol className="progression">
              <li>
                <a href="#admin">
                  <span>1</span>Admin Console
                </a>
              </li>
              <li>
                <a href="#partner">
                  <span>2</span>Demo Partner
                </a>
              </li>
              <li>
                <a href="#selector">
                  <span>3</span>Selector
                </a>
              </li>
              <li>
                <a href="#checkout">
                  <span>4</span>Checkout + Ticket
                </a>
              </li>
              <li>
                <a href="#scanner">
                  <span>5</span>Scanner
                </a>
              </li>
              <li>
                <a href="#technical">
                  <span>6</span>Technical Review
                </a>
              </li>
            </ol>
          </nav>
        </section>

        <section className="walkthrough" aria-labelledby="walkthrough-title">
          <div className="walkthrough-heading">
            <p className="eyebrow">Review checklist</p>
            <h2 id="walkthrough-title">Follow Steps 1–6</h2>
            <p>Complete each functional step before moving to the next one.</p>
          </div>

          <ol className="steps">
            {steps.map((step, index) => (
              <li id={step.id} key={step.id} className="review-step">
                <div className="step-marker" aria-hidden="true">
                  {index + 1}
                </div>
                <article className="step-content">
                  <header className="step-header">
                    <div>
                      <p className={step.demo ? 'surface-label demo' : 'surface-label'}>
                        {step.surface}
                      </p>
                      <h3>Review {step.name}</h3>
                    </div>
                    {step.demo && <span className="demo-badge">Not part of TktSync</span>}
                  </header>
                  <p className="step-description">{step.description}</p>
                  <div className="instruction-grid">
                    <section aria-labelledby={`${step.id}-do`}>
                      <h4 id={`${step.id}-do`}>What to do</h4>
                      <ul>
                        {step.review.map((item) => (
                          <li key={item}>{item}</li>
                        ))}
                      </ul>
                    </section>
                    <section className="verify" aria-labelledby={`${step.id}-verify`}>
                      <h4 id={`${step.id}-verify`}>What to verify</h4>
                      <ul>
                        {step.verify.map((item) => (
                          <li key={item}>{item}</li>
                        ))}
                      </ul>
                    </section>
                  </div>
                  {step.access && <AccessDetails />}
                  <a className="step-link" href={step.href} target="_blank" rel="noreferrer">
                    {step.action} <span aria-hidden="true">→</span>
                  </a>
                </article>
              </li>
            ))}

            <li id="technical" className="review-step">
              <div className="step-marker" aria-hidden="true">
                6
              </div>
              <article className="step-content">
                <header className="step-header">
                  <div>
                    <p className="surface-label">TktSync product and implementation</p>
                    <h3>Developer / Technical Review</h3>
                  </div>
                </header>
                <p className="step-description">
                  Complete the functional walkthrough first, then review the API contract and the
                  implementation decisions that protect inventory, confirmation and admission.
                </p>
                <div className="instruction-grid">
                  <section aria-labelledby="technical-do">
                    <h4 id="technical-do">What to do</h4>
                    <ul>
                      <li>Open Developer Docs and follow the Partner integration workflow.</li>
                      <li>Review the repository structure and system architecture.</li>
                      <li>Inspect the security and production runtime models.</li>
                    </ul>
                  </section>
                  <section className="verify" aria-labelledby="technical-verify">
                    <h4 id="technical-verify">What to verify</h4>
                    <ul>
                      <li>Partner credentials and Reservation tokens remain server-side.</li>
                      <li>
                        Inventory, ticket issuance and admission share one authoritative model.
                      </li>
                    </ul>
                  </section>
                </div>
                <div className="technical-links">
                  <a
                    className="step-link primary-link"
                    href={config.docs}
                    target="_blank"
                    rel="noreferrer"
                  >
                    Open Developer Docs <span aria-hidden="true">→</span>
                  </a>
                  <a href={config.source} target="_blank" rel="noreferrer">
                    Source code ↗
                  </a>
                  <a href={config.architecture} target="_blank" rel="noreferrer">
                    Architecture ↗
                  </a>
                  <a href={config.security} target="_blank" rel="noreferrer">
                    Security ↗
                  </a>
                  <a href={config.runtime} target="_blank" rel="noreferrer">
                    Runtime &amp; concurrency ↗
                  </a>
                </div>
              </article>
            </li>
          </ol>
        </section>
      </main>

      <footer>
        <span>TktSync assessment review</span>
        <span>Admin → Partner → Selector → Ticket → Scanner → Technical review</span>
      </footer>
    </>
  );
}

export default App;
