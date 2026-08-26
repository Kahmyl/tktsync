import { useEffect, useMemo, useState } from 'react';
import { getReviewConfig } from './config';

const config = getReviewConfig();

type ReviewStep = {
  id: string;
  name: string;
  summary: string;
  href: string;
  openLabel: string;
  demo?: boolean;
  tasks: string[];
  result: string;
};

const steps: ReviewStep[] = [
  {
    id: 'admin',
    name: 'Admin Console',
    summary: 'Review the operational side of TktSync first.',
    href: config.admin,
    openLabel: 'Open Admin Console',
    tasks: [
      'Sign in with the review account shown on this page.',
      'Open the prepared Event and review its venue, published layout, pricing and inventory.',
      'Review Partner access, allocations/restrictions and Event transaction settings.',
      'Open reporting and audit views to see the operational record behind the Event.',
    ],
    result: 'You should leave this step understanding how TktSync configures and owns Event inventory.',
  },
  {
    id: 'partner',
    name: 'Demo Partner Storefront',
    summary: 'Review the integration from an independent ticket seller.',
    href: config.partner,
    openLabel: 'Open Demo Partner',
    demo: true,
    tasks: [
      'Open the prepared Event in the Demo Partner storefront.',
      'Confirm that the Event and ticket options are loaded from TktSync.',
      'Choose tickets to continue into the TktSync Selector.',
    ],
    result:
      'The Demo Partner is not part of TktSync; it demonstrates how an external seller uses the real Partner API.',
  },
  {
    id: 'selector',
    name: 'TktSync Selector',
    summary: 'Review buyer selection against authoritative inventory.',
    href: config.partner,
    openLabel: 'Start from Demo Partner',
    tasks: [
      'From the Demo Partner, choose tickets and enter the TktSync Selector.',
      'Select an available reserved seat or general-admission quantity.',
      'Create a hold and continue back to the Partner checkout.',
    ],
    result:
      'The Selector is where TktSync checks current availability and protects inventory before checkout.',
  },
  {
    id: 'checkout',
    name: 'Partner Checkout & Ticket',
    summary: 'Complete the sale and inspect the resulting ticket.',
    href: config.partner,
    openLabel: 'Continue Demo Partner Journey',
    demo: true,
    tasks: [
      'Continue the held reservation through the Demo Partner checkout.',
      'Use the simulated payment-success action.',
      'Open the issued ticket and inspect its ticket identity, seat information and hosted QR.',
    ],
    result:
      'The Partner owns checkout/payment presentation; TktSync confirms the sale and remains the ticket and QR authority.',
  },
  {
    id: 'scanner',
    name: 'Scanner',
    summary: 'Review online admission and duplicate-scan prevention.',
    href: config.scanner,
    openLabel: 'Open Scanner',
    tasks: [
      'Sign in with the review account.',
      'Select the Event used in the previous steps.',
      'Scan or manually enter the ticket credential created in the Partner flow.',
      'Submit the same credential again to verify duplicate admission is rejected.',
    ],
    result: 'The first valid admission succeeds; subsequent use of the same ticket is rejected.',
  },
  {
    id: 'docs',
    name: 'Partner Developer Documentation',
    summary: 'Finish by reviewing how a real Partner integrates with TktSync.',
    href: config.docs,
    openLabel: 'Open Developer Docs',
    tasks: [
      'Review authentication and the Partner workflow guide.',
      'Inspect Event/availability, selection, reservation, checkout confirmation and ticket endpoints.',
      'Review idempotency, errors and webhook guidance.',
    ],
    result: 'This is the integration surface an external ticketing platform would build against.',
  },
];

function stepFromHash() {
  const hash = window.location.hash.replace(/^#\/?/, '');
  return steps.some((step) => step.id === hash) ? hash : '';
}

function App() {
  const [activeStepID, setActiveStepID] = useState(stepFromHash);
  const credentialsShown = Boolean(config.accessEmail && config.accessPassword);
  const activeIndex = steps.findIndex((step) => step.id === activeStepID);
  const activeStep = activeIndex >= 0 ? steps[activeIndex] : undefined;

  useEffect(() => {
    const onHashChange = () => setActiveStepID(stepFromHash());
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  }, []);

  const nextStep = useMemo(
    () => (activeIndex >= 0 ? steps[activeIndex + 1] : undefined),
    [activeIndex],
  );

  const openGuide = (id: string) => {
    window.location.hash = id;
    setActiveStepID(id);
    window.scrollTo(0, 0);
  };

  const backToReview = () => {
    history.pushState(null, '', `${window.location.pathname}${window.location.search}`);
    setActiveStepID('');
    window.scrollTo(0, 0);
  };

  if (activeStep) {
    return (
      <>
        <header className="top">
          <button className="brand brand-button" type="button" onClick={backToReview}>
            <span>T</span>TktSync <small>review</small>
          </button>
          <button className="text-button" type="button" onClick={backToReview}>
            ← Review overview
          </button>
        </header>

        <main className="guide-page">
          <div className="guide-page-inner">
            <p className="step-kicker">
              Step {activeIndex + 1} of {steps.length}
              {activeStep.demo && <span className="demo-label">Demo only</span>}
            </p>
            <h1>Review {activeStep.name}</h1>
            <p className="guide-summary">{activeStep.summary}</p>

            <section className="task-panel" aria-labelledby="tasks-title">
              <h2 id="tasks-title">What to do</h2>
              <ol className="task-list">
                {activeStep.tasks.map((task) => (
                  <li key={task}>{task}</li>
                ))}
              </ol>
              <div className="expected-result">
                <strong>What this demonstrates</strong>
                <p>{activeStep.result}</p>
              </div>
            </section>

            {(activeStep.id === 'admin' || activeStep.id === 'scanner') &&
              (credentialsShown || config.accessInstructions) && (
                <section className="inline-access" aria-labelledby="guide-access-title">
                  <h2 id="guide-access-title">Review access</h2>
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
                  {config.accessInstructions && <p>{config.accessInstructions}</p>}
                </section>
              )}

            <div className="guide-actions">
              <a className="primary action-button" href={activeStep.href} target="_blank" rel="noreferrer">
                {activeStep.openLabel} ↗
              </a>
              {nextStep ? (
                <button className="secondary action-button" type="button" onClick={() => openGuide(nextStep.id)}>
                  Next: {nextStep.name} →
                </button>
              ) : (
                <button className="secondary action-button" type="button" onClick={backToReview}>
                  Back to review overview
                </button>
              )}
            </div>
          </div>
        </main>
      </>
    );
  }

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
        <section className="review-intro">
          <p className="section-label">Assessment review</p>
          <h1>Review TktSync end to end</h1>
          <p>
            Follow the six steps below in order. Each step first explains exactly what to review,
            then links you to the relevant application.
          </p>
        </section>

        <section className="review-sequence" aria-labelledby="review-sequence-title">
          <div className="section-heading">
            <h2 id="review-sequence-title">Review sequence</h2>
            <p>Complete every step. The sequence moves from Event setup through sale and admission.</p>
          </div>

          <ol className="review-list">
            {steps.map((step, index) => (
              <li key={step.id}>
                <div className="review-number">{index + 1}</div>
                <div className="review-copy">
                  <p>
                    {step.name}
                    {step.demo && <span className="demo-label">Demo only</span>}
                  </p>
                  <h3>{step.summary}</h3>
                  <button className="review-button" type="button" onClick={() => openGuide(step.id)}>
                    Review {step.name} →
                  </button>
                </div>
              </li>
            ))}
          </ol>
        </section>

        <section className="boundary compact-section" aria-labelledby="boundary-title">
          <p className="section-label">Important boundary</p>
          <h2 id="boundary-title">The Demo Partner is only a reference application</h2>
          <p>
            Admin, Selector, Scanner, Partner API and Developer Documentation are TktSync surfaces.
            The Demo Partner exists only to demonstrate how an independent seller integrates with them.
          </p>
        </section>

        {(credentialsShown || config.accessInstructions) && (
          <section className="access compact-section" aria-labelledby="access-title">
            <p className="section-label">Reviewer access</p>
            <h2 id="access-title">Credentials used during the review</h2>
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
            {config.accessInstructions && <p className="access-note">{config.accessInstructions}</p>}
          </section>
        )}

        <section className="technical compact-section" aria-labelledby="technical-title">
          <p className="section-label">Implementation references</p>
          <h2 id="technical-title">Technical material</h2>
          <div className="tech-links">
            <a href={config.source} target="_blank" rel="noreferrer">
              <strong>Source code</strong>
              <span>Repository ↗</span>
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
