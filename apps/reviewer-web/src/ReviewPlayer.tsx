/* eslint-disable react-refresh/only-export-components */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { phases } from './App';
import { getReviewConfig } from './config';

const config = getReviewConfig();
const eventKey = 'tktsync-review-event-name';
const defaultEventName = 'Reviewer Walkthrough Event';

type ActionSlide = {
  kind: 'action';
  id: string;
  phase: (typeof phases)[number];
  phaseIndex: number;
  action: (typeof phases)[number]['actions'][number];
  actionIndex: number;
};
type Slide =
  | { kind: 'welcome'; id: string }
  | { kind: 'boundary'; id: string }
  | { kind: 'technical'; id: string }
  | { kind: 'finish'; id: string }
  | ActionSlide;

export function buildSlides(): Slide[] {
  return [
    { kind: 'welcome', id: 'welcome' },
    { kind: 'boundary', id: 'boundary' },
    ...phases.flatMap((phase, phaseIndex) =>
      phase.actions.map((action, actionIndex) => ({
        kind: 'action' as const,
        id: `${phase.id}-${actionIndex + 1}`,
        phase,
        phaseIndex,
        action,
        actionIndex,
      })),
    ),
    { kind: 'technical', id: 'technical' },
    { kind: 'finish', id: 'finish' },
  ];
}

function Logo() {
  return (
    <span className="wordmark">
      <span className="logo-ticket" aria-hidden="true">
        ⇄
      </span>
      <b>
        Tkt<span>Sync</span>
      </b>
    </span>
  );
}

function AccessDetails() {
  if (!config.accessEmail && !config.accessPassword && !config.accessInstructions) return null;
  return (
    <aside className="access-card">
      <div>
        <p className="section-label">Reviewer access</p>
        {config.accessInstructions && <p>{config.accessInstructions}</p>}
      </div>
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
    </aside>
  );
}

export default function ReviewPlayer() {
  const slides = useMemo(buildSlides, []);
  const [index, setIndex] = useState(() => {
    if (typeof window === 'undefined') return 0;
    const found = slides.findIndex((slide) => slide.id === window.location.hash.slice(1));
    return found < 0 ? 0 : found;
  });
  const [direction, setDirection] = useState<'forward' | 'back'>('forward');
  const [eventName, setEventName] = useState(() =>
    typeof window === 'undefined'
      ? defaultEventName
      : window.localStorage.getItem(eventKey) || defaultEventName,
  );
  const slide = slides[index]!;

  const move = useCallback(
    (requested: number) => {
      const next = Math.max(0, Math.min(slides.length - 1, requested));
      setDirection(next < index ? 'back' : 'forward');
      setIndex(next);
      if (typeof window !== 'undefined')
        window.history.replaceState(null, '', `#${slides[next]!.id}`);
    },
    [index, slides],
  );

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.target as HTMLElement | null)?.matches('input,textarea,select,button,a')) return;
      if (event.key === 'ArrowRight') move(index + 1);
      if (event.key === 'ArrowLeft') move(index - 1);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [index, move]);

  const changeEventName = (value: string) => {
    const next = value.slice(0, 80);
    setEventName(next);
    window.localStorage.setItem(eventKey, next);
  };

  const content = () => {
    if (slide.kind === 'welcome')
      return (
        <section className="welcome-slide">
          <p className="eyebrow">Assessment walkthrough</p>
          <h1>Follow one ticket from setup to the gate.</h1>
          <p className="lead-copy">
            TktSync keeps Event inventory accurate across independent ticketing platforms, so the
            same ticket cannot be sold twice. This guide gives you one focused action at a time.
          </p>
          <div className="journey-line" aria-label="Complete journey">
            <span>Venue</span>
            <i>→</i>
            <span>Event</span>
            <i>→</i>
            <span>Partner</span>
            <i>→</i>
            <span>Selector</span>
            <i>→</i>
            <span>Ticket</span>
            <i>→</i>
            <span>Scanner</span>
          </div>
          <button className="primary-button" onClick={() => move(1)}>
            Start guided review <span>→</span>
          </button>
        </section>
      );
    if (slide.kind === 'boundary')
      return (
        <section>
          <p className="eyebrow">Before you begin</p>
          <h1>Know which application you are looking at.</h1>
          <div className="boundary-grid">
            <article>
              <p className="surface-label">TktSync product</p>
              <h2>Admin · Selector · Scanner · Partner API · Docs</h2>
              <p>
                These surfaces own Event inventory, Reservations, tickets and admission decisions.
              </p>
            </article>
            <article className="demo-boundary">
              <p className="surface-label demo">Demo-only application</p>
              <h2>Connected Partner storefront</h2>
              <p>
                This sample storefront is not part of TktSync. It demonstrates how an independent
                ticketing company uses the real Partner API.
              </p>
            </article>
          </div>
        </section>
      );
    if (slide.kind === 'technical')
      return (
        <section>
          <p className="eyebrow">Public documentation review</p>
          <h1>Review the public Partner integration guide.</h1>
          <p className="lead-copy">
            Complete this step using only the deployed Developer Docs. Do not inspect source code or
            internal repository documents as part of this guided assessment.
          </p>
          <div className="instruction-layout">
            <div>
              <p className="section-label">Do this</p>
              <ol className="action-instructions">
                <li>Open the public Developer Docs using the button below.</li>
                <li>
                  Read the Partner workflow from Event discovery through Selector, Reservation,
                  checkout confirmation, ticket issuance and admission.
                </li>
                <li>
                  Confirm that Partner credentials stay server-side and the Reservation token
                  returns to Partner checkout through a secure form POST rather than the URL.
                </li>
                <li>
                  Review how hosted QR credentials and Scanner validation complete the same
                  authoritative ticket lifecycle.
                </li>
              </ol>
            </div>
            <aside className="result-card">
              <p className="section-label">Done when</p>
              <p>
                The public docs explain every handoff you just completed in the visible journey,
                without requiring source-code or internal-document inspection.
              </p>
              <div className="action-note">
                <strong>Review boundary</strong>
                <p>
                  Developer Docs are required. Do not open repository or source-code material during
                  this guided assessment.
                </p>
              </div>
            </aside>
          </div>
          <a className="phase-cta" href={config.docs} target="_blank" rel="noreferrer">
            Open Developer Docs <span aria-hidden="true">↗</span>
          </a>
        </section>
      );
    if (slide.kind === 'finish')
      return (
        <section className="finish-slide">
          <span className="finish-mark" aria-hidden="true">
            ✓
          </span>
          <p className="eyebrow">Guided review complete</p>
          <h1>You have followed the complete ticket journey.</h1>
          <p className="lead-copy">
            Return to any phase from the phase rail, reopen the documentation, or restart the guide.
          </p>
          <div className="finish-actions">
            <button className="primary-button" onClick={() => move(0)}>
              Restart guide
            </button>
            <a className="secondary-button" href={config.docs} target="_blank" rel="noreferrer">
              Open Developer Docs ↗
            </a>
          </div>
        </section>
      );

    const { phase, phaseIndex, action, actionIndex } = slide;
    const isEventCreation = action.title === 'Create the Event draft';
    return (
      <section className="action-slide">
        <div className="action-heading">
          <div>
            <p className={phase.demo ? 'surface-label demo' : 'surface-label'}>{phase.surface}</p>
            <p className="phase-name">
              Phase {phaseIndex + 1} · {phase.name}
            </p>
            <h1>{action.title}</h1>
          </div>
          {phase.demo && <span className="demo-badge">Not part of TktSync</span>}
        </div>
        <p className="location">
          <span aria-hidden="true">⌁</span>
          {action.location}
        </p>
        {phase.access && actionIndex === 0 && <AccessDetails />}
        {isEventCreation && (
          <div className="review-context">
            <label htmlFor="review-event-name">Use this Event name throughout the review</label>
            <input
              id="review-event-name"
              value={eventName}
              onChange={(event) => changeEventName(event.target.value)}
            />
            <p>
              The guide remembers this name on this device and refers to it in every later phase.
            </p>
          </div>
        )}
        <div className="instruction-layout">
          <div>
            <p className="section-label">Do this</p>
            <ol className="action-instructions">
              {action.instructions.map((instruction) => (
                <li key={instruction}>
                  {isEventCreation && instruction.includes('review Event name shown')
                    ? instruction.replace(
                        'the review Event name shown in this guide',
                        `“${eventName || defaultEventName}”`,
                      )
                    : instruction}
                </li>
              ))}
            </ol>
          </div>
          <aside className="result-card">
            <p className="section-label">Done when</p>
            <p>{action.complete}</p>
            {action.note && (
              <div className="action-note">
                <strong>Important</strong>
                <p>{action.note}</p>
              </div>
            )}
          </aside>
        </div>
        <a className="phase-cta" href={action.href || phase.href} target="_blank" rel="noreferrer">
          {action.cta || phase.action} <span aria-hidden="true">↗</span>
        </a>
      </section>
    );
  };

  const percent = Math.round(((index + 1) / slides.length) * 100);
  return (
    <div className="review-app">
      <header className="top">
        <button className="brand" onClick={() => move(0)} aria-label="Return to guide start">
          <Logo />
          <strong>Review Guide</strong>
        </button>
        <a className="source-link" href={config.docs} target="_blank" rel="noreferrer">
          Developer Docs ↗
        </a>
      </header>
      <div className="progress-track">
        <span style={{ width: `${percent}%` }} />
      </div>
      <main className="player-shell">
        <aside className="phase-rail" aria-label="Review phases">
          <p>Guided review</p>
          <ol>
            {phases.map((phase, phaseIndex) => {
              const first = slides.findIndex(
                (item) => item.kind === 'action' && item.phase.id === phase.id,
              );
              const active = slide.kind === 'action' && slide.phase.id === phase.id;
              return (
                <li key={phase.id}>
                  <button className={active ? 'active' : ''} onClick={() => move(first)}>
                    <span>{phaseIndex + 1}</span>
                    <b>{phase.shortName}</b>
                  </button>
                </li>
              );
            })}
            <li>
              <button
                className={slide.kind === 'technical' ? 'active' : ''}
                onClick={() => move(slides.findIndex((item) => item.kind === 'technical'))}
              >
                <span>7</span>
                <b>Docs</b>
              </button>
            </li>
          </ol>
        </aside>
        <div className="player">
          <div className="step-meta">
            <span>
              {slide.kind === 'action'
                ? `Phase ${slide.phaseIndex + 1} · Action ${slide.actionIndex + 1} of ${slide.phase.actions.length}`
                : slide.kind === 'welcome'
                  ? 'Start here'
                  : slide.kind === 'finish'
                    ? 'Complete'
                    : `Step ${index + 1}`}
            </span>
            <span>
              {index + 1} of {slides.length}
            </span>
          </div>
          <article key={slide.id} className={`slide-card slide-${direction}`}>
            {content()}
          </article>
          <nav className="player-controls" aria-label="Guide controls">
            <button
              className="previous-button"
              onClick={() => move(index - 1)}
              disabled={index === 0}
            >
              ← Previous
            </button>
            <span className="mobile-progress">
              {index + 1} / {slides.length}
            </span>
            <button
              className="next-button"
              onClick={() => move(index + 1)}
              disabled={index === slides.length - 1}
            >
              {slide.kind === 'welcome' ? 'Start' : slide.kind === 'technical' ? 'Finish' : 'Next'}{' '}
              →
            </button>
          </nav>
        </div>
      </main>
    </div>
  );
}
