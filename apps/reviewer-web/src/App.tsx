import { useState } from 'react';
import { getReviewConfig } from './config';

const config = getReviewConfig();

const steps = [
  {
    name: 'Admin Console',
    title: 'See how an Event is configured',
    what: 'The TktSync operations interface for venues, Events, pricing and inventory.',
    try: 'Open the prepared demo Event and inspect its layout, pricing and inventory.',
    notice: 'The published layout becomes the same ticket map buyers use later.',
    href: config.admin,
    action: 'Open Admin Console',
    full: true,
  },
  {
    name: 'Demo Partner Storefront',
    title: 'Experience an independent ticket seller',
    what: 'A sample external storefront built only for this assessment.',
    try: 'Open the live Event, read its details and choose tickets.',
    notice: 'The Partner owns the storefront; its Event data comes from TktSync.',
    href: config.partner,
    action: 'Open Demo Partner',
    full: false,
    demo: true,
  },
  {
    name: 'TktSync Selector',
    title: 'Select tickets as a buyer',
    what: 'The TktSync-hosted ticket picker opened by the Partner.',
    try: 'Choose an available seat or ticket quantity, then hold it.',
    notice: 'TktSync checks and holds the authoritative inventory.',
    href: config.partner,
    action: 'Start from Demo Partner',
    full: false,
  },
  {
    name: 'Partner ticket',
    title: 'Complete checkout and view the ticket',
    what: 'The Demo Partner’s checkout and ticket presentation around a TktSync ticket.',
    try: 'Simulate payment success, then view the issued ticket and hosted QR.',
    notice: 'Payment belongs to the Partner; ticket identity and QR authority remain with TktSync.',
    href: config.partner,
    action: 'Continue ticket journey',
    full: false,
  },
  {
    name: 'Scanner',
    title: 'Validate entry',
    what: 'The TktSync admission interface used at the venue.',
    try: 'Open Scanner and scan the QR on the ticket you just created.',
    notice: 'The first valid scan is admitted; a duplicate is rejected.',
    href: config.scanner,
    action: 'Open Scanner',
    full: false,
  },
  {
    name: 'Partner Developer Docs',
    title: 'Explore the integration',
    what: 'The API guide an external ticketing company uses to integrate.',
    try: 'Read the guided workflow and inspect the Partner endpoints.',
    notice: 'Partner credentials remain server-side throughout the journey.',
    href: config.docs,
    action: 'Open Developer Docs',
    full: true,
  },
];

function App() {
  const [mode, setMode] = useState<'quick' | 'full'>('quick');
  const visible = mode === 'full' ? steps : steps.filter((step) => !step.full);
  const credentialsShown = Boolean(config.accessEmail && config.accessPassword);

  return (
    <>
      <header className="top">
        <a className="brand" href="#top">
          <span>T</span>TktSync <small>review</small>
        </a>
        <a href={config.source} target="_blank" rel="noreferrer">
          Source code ↗
        </a>
      </header>
      <main id="top">
        <section className="hero" aria-labelledby="hero-title">
          <p className="kicker">Assessment walkthrough</p>
          <h1 id="hero-title">One inventory truth across multiple ticketing platforms.</h1>
          <p>
            TktSync coordinates Event inventory across independent ticketing Partners so the same
            seat or ticket cannot be sold twice.
          </p>
          <div className="hero-actions">
            <a className="primary" href="#guided">
              Start guided review
            </a>
            <a className="secondary" href={config.source} target="_blank" rel="noreferrer">
              View source code
            </a>
          </div>
          <p className="time">No technical background required · 3–15 minutes</p>
        </section>

        <section className="flow-section" aria-labelledby="flow-title">
          <p className="section-label">What you’re about to see</p>
          <h2 id="flow-title">One complete ticket journey</h2>
          <ol className="flow">
            <li>Event setup</li>
            <li>Partner storefront</li>
            <li>Buyer selects tickets</li>
            <li>Partner checkout</li>
            <li>TktSync issues ticket + QR</li>
            <li>Scanner validates admission</li>
          </ol>
          <p className="flow-note">
            TktSync sits underneath the Partner experience and remains the authoritative source for
            inventory, tickets and admission.
          </p>
        </section>

        <section id="guided" className="guide" aria-labelledby="guide-title">
          <div className="guide-head">
            <div>
              <p className="section-label">Guided review</p>
              <h2 id="guide-title">Follow these steps in order</h2>
              <p>Each step tells you what to try and what the result demonstrates.</p>
            </div>
            <div className="mode" aria-label="Review length">
              <button className={mode === 'quick' ? 'active' : ''} onClick={() => setMode('quick')}>
                <strong>Quick review</strong>
                <span>3–5 min · 4 steps</span>
              </button>
              <button className={mode === 'full' ? 'active' : ''} onClick={() => setMode('full')}>
                <strong>Full review</strong>
                <span>10–15 min · 6 steps</span>
              </button>
            </div>
          </div>
          <ol className="steps">
            {visible.map((step, index) => (
              <li key={step.name}>
                <div className="step-number">{index + 1}</div>
                <div className="step-body">
                  <div className="step-title">
                    <div>
                      <p>
                        {step.name}
                        {step.demo && <span className="demo-label">Demo only</span>}
                      </p>
                      <h3>{step.title}</h3>
                    </div>
                  </div>
                  <dl>
                    <div>
                      <dt>What it is</dt>
                      <dd>{step.what}</dd>
                    </div>
                    <div>
                      <dt>Try</dt>
                      <dd>{step.try}</dd>
                    </div>
                    <div>
                      <dt>You should notice</dt>
                      <dd>{step.notice}</dd>
                    </div>
                  </dl>
                  <a className="step-link" href={step.href} target="_blank" rel="noreferrer">
                    {step.action} <span>→</span>
                  </a>
                </div>
              </li>
            ))}
          </ol>
        </section>

        <section className="boundary" aria-labelledby="boundary-title">
          <div>
            <p className="section-label">Product boundary</p>
            <h2 id="boundary-title">What is part of TktSync?</h2>
            <p>The walkthrough includes one intentionally separate reference application.</p>
          </div>
          <div className="boundary-grid">
            <div>
              <h3>TktSync product surfaces</h3>
              <ul>
                <li>Admin Console</li>
                <li>Buyer Selector</li>
                <li>Scanner</li>
                <li>Partner API</li>
                <li>Developer Documentation</li>
              </ul>
            </div>
            <div className="demo-box">
              <p>◇ Demo-only surface</p>
              <h3>Demo Partner Storefront</h3>
              <p>
                This sample storefront is not part of TktSync. It exists only to show how an
                independent ticketing platform uses the real TktSync Partner API.
              </p>
            </div>
          </div>
        </section>

        {(credentialsShown || config.accessInstructions) && (
          <section className="access" aria-labelledby="access-title">
            <p className="section-label">Reviewer access</p>
            <h2 id="access-title">Ready-to-use assessment access</h2>
            {config.accessLabel && <p>{config.accessLabel}</p>}
            {credentialsShown && (
              <div className="credentials">
                <div>
                  <span>Email</span>
                  <code>{config.accessEmail}</code>
                </div>
                <div>
                  <span>Password</span>
                  <code>{config.accessPassword}</code>
                </div>
              </div>
            )}
            {config.accessInstructions && (
              <p className="access-note">{config.accessInstructions}</p>
            )}
            <p className="safety">
              These values are deployment configuration for a limited review account; no credentials
              are stored in this source code.
            </p>
          </section>
        )}

        <section className="technical" aria-labelledby="technical-title">
          <p className="section-label">Technical review</p>
          <h2 id="technical-title">Continue into the implementation</h2>
          <div className="tech-links">
            <a href={config.source} target="_blank" rel="noreferrer">
              <strong>Source code</strong>
              <span>Repository and setup ↗</span>
            </a>
            <a href={config.architecture} target="_blank" rel="noreferrer">
              <strong>Architecture</strong>
              <span>System design ↗</span>
            </a>
            <a href={config.docs} target="_blank" rel="noreferrer">
              <strong>API documentation</strong>
              <span>Partner integration ↗</span>
            </a>
            <a href={config.security} target="_blank" rel="noreferrer">
              <strong>Security model</strong>
              <span>Trust boundaries ↗</span>
            </a>
            <a href={config.runtime} target="_blank" rel="noreferrer">
              <strong>Runtime & concurrency</strong>
              <span>Correctness model ↗</span>
            </a>
          </div>
        </section>
      </main>
      <footer>
        <span>TktSync assessment review</span>
        <a href={config.source} target="_blank" rel="noreferrer">
          github.com/Kahmyl/tktsync
        </a>
      </footer>
    </>
  );
}

export default App;
