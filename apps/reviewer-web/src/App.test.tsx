import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import App from './App';

describe('reviewer walkthrough', () => {
  const markup = renderToStaticMarkup(<App />);

  it('presents the complete journey in its required order', () => {
    const headings = [
      'Configure an Event in Admin',
      'Enter the Demo Partner Storefront',
      'Select and Hold Real Inventory',
      'Complete Checkout and Inspect the Ticket',
      'Validate Admission in Scanner',
      'Return to Admin and Verify the Outcome',
    ];

    headings.reduce((lastPosition, heading) => {
      const position = markup.indexOf(heading);
      expect(position).toBeGreaterThan(lastPosition);
      return position;
    }, -1);
  });

  it('makes the Partner demo boundary unmistakable', () => {
    expect(markup).toContain('Not part of TktSync');
    expect(markup).toContain('sample storefront is not part of TktSync');
    expect(markup).toContain('real Partner API');
  });

  it('teaches prerequisites, pricing, admission and post-sale proof', () => {
    expect(markup).toContain('Do not start with “Create event”');
    expect(markup).toContain('Materialize the published layout');
    expect(markup).toContain('entering 25000 should display ₦25,000');
    expect(markup).toContain('Already admitted');
    expect(markup).toContain('Admin → Reports, then Dashboard');
  });

  it('includes source code and technical review links', () => {
    expect(markup).toContain('github.com/Kahmyl/tktsync');
    expect(markup).toContain('Security model');
    expect(markup).toContain('Runtime model');
  });
});
