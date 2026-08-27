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
    expect(create?.instructions.join(' ')).toContain('all six date and time fields');
    expect(create?.instructions.join(' ')).toContain('10 minutes before the current time');
    expect(create?.instructions.join(' ')).toContain('do not use a future Sales open time');
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
});
