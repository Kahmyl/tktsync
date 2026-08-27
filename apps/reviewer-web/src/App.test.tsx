import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import App, { phases } from './App';
import { buildSlides } from './ReviewPlayer';

describe('reviewer walkthrough player', () => {
  it('starts with one focused view and exposes sequential controls', () => {
    const markup = renderToStaticMarkup(<App />);
    expect(markup).toContain('Follow one ticket from setup to the gate.');
    expect(markup).toContain('Start guided review');
    expect(markup).toContain('aria-label="Guide controls"');
    expect(markup).not.toContain('Create the venue');
  });

  it('keeps every phase and action in the required order', () => {
    const slides = buildSlides().filter((slide) => slide.kind === 'action');
    expect(slides.map((slide) => slide.phase.name)).toEqual(
      phases.flatMap((phase) => phase.actions.map(() => phase.name)),
    );
    expect(slides.length).toBe(phases.reduce((sum, phase) => sum + phase.actions.length, 0));
  });

  it('makes the Partner boundary and Event identity unambiguous', () => {
    const copy = JSON.stringify(phases);
    expect(copy).toContain('Demo-only application');
    expect(copy).toContain('Open the Event you created');
    expect(copy).not.toContain('Championship Night');
    expect(copy).not.toContain('Open the prepared Event');
  });

  it('includes schedule verification and recovery before Event creation', () => {
    const create = phases[0]!.actions.find((action) => action.title === 'Create the Event draft');
    expect(create?.instructions.join(' ')).toContain('any Event name you want');
    expect(create?.instructions.join(' ')).not.toContain('Reviewer Walkthrough Event');
    expect(create?.instructions.join(' ')).toContain('all six date and time fields');
    expect(create?.instructions.join(' ')).toContain('Review timing');
    expect(create?.instructions.join(' ')).toContain(
      'both Sales open and Admission open to at least 10 minutes before the current time',
    );
    expect(create?.instructions.join(' ')).toContain('let you buy a ticket and test Scanner');
    expect(create?.instructions.join(' ')).toContain('26 October');
    expect(create?.instructions.join(' ')).toContain('Entry is not open until 26 October arrives');
    expect(create?.instructions.join(' ')).toContain(
      'wait until that time before the Partner storefront can sell the ticket',
    );
    expect(create?.instructions.join(' ')).toContain('If any line says Not scheduled');
  });

  it('prevents scheduled sales from blocking the Partner journey', () => {
    const readiness = phases[0]!.actions.find((action) => action.title.includes('readiness check'));
    const instructions = readiness?.instructions.join(' ') || '';
    expect(instructions).toContain('primary action says Open sales');
    expect(instructions).toContain('If it says Schedule sales');
    expect(instructions).toContain('do not continue to the Partner storefront');
    expect(readiness?.complete).toContain('On sale');
  });

  it('keeps admission open for the live Scanner proof and identifies the desktop code', () => {
    const create = phases[0]!.actions.find((action) => action.title === 'Create the Event draft');
    expect(create?.instructions.join(' ')).toContain(
      'Admission open to at least 10 minutes before the current time',
    );
    expect(create?.note).toContain('complete the purchase and Scanner proof now');

    const scanner = phases.find((phase) => phase.id === 'scanner');
    expect(JSON.stringify(scanner)).toContain('Manual admission code');
    expect(JSON.stringify(scanner)).toContain('desktop or laptop cannot use the QR camera scanner');
    expect(JSON.stringify(scanner)).toContain('scan the ticket QR displayed on another screen');
    expect(JSON.stringify(scanner)).toContain('Do not enter the public Ticket ID');
    expect(JSON.stringify(scanner)).toContain('Entry is not open');
  });

  it('warns the reviewer to save the one-time Partner credential before closing it', () => {
    const credential = phases[0]!.actions.find((action) =>
      action.title.includes('save its credential'),
    );
    const instructions = credential?.instructions.join(' ') || '';
    expect(instructions).toContain('save it somewhere temporary and secure');
    expect(instructions).toContain('Do not choose I have stored it');
    expect(instructions).toContain('/checkout/return');
    expect(instructions).toContain('Save checkout URLs');
    expect(credential?.note).toContain('If it is lost, you must issue a new credential');
  });

  it('explains the Partner display-name and credential boundary', () => {
    const connection = phases[1]!.actions.find((action) =>
      action.title.includes('Partner storefront'),
    );
    expect(connection?.instructions.join(' ')).toContain(
      'credential is what securely identifies the Partner',
    );
    expect(connection?.note).toContain('deployment configuration problem');
  });

  it('routes stable guide actions directly to their named application screens', () => {
    const admin = phases[0]!;
    expect(admin.actions.find((action) => action.title === 'Create the venue')?.href).toMatch(
      /\/venues$/,
    );
    expect(admin.actions.find((action) => action.title === 'Create the Event draft')?.href).toMatch(
      /\/events\/new$/,
    );
    expect(
      admin.actions.find((action) => action.title.includes('save its credential'))?.href,
    ).toMatch(/\/partners$/);
  });

  it('keeps the required final review inside public Developer Docs', () => {
    const markup = renderToStaticMarkup(<App />);
    expect(markup).toContain('Developer Docs');
    expect(markup).not.toContain('Source code');

    const technical = buildSlides().find((slide) => slide.kind === 'technical');
    expect(technical?.kind).toBe('technical');
  });
});
