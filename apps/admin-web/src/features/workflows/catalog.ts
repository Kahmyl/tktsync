export type Workflow = {
  group: string;
  title: string;
  description: string;
  method: 'GET' | 'POST' | 'PATCH' | 'PUT';
  path: string;
  body: string;
};
export const workflows: Workflow[] = [
  {
    group: 'Build',
    title: 'Venues',
    description: 'Create venues and inspect operational inventory.',
    method: 'POST',
    path: '/api/v1/admin/venues',
    body: '{"name":"Harbour Arena","timezone_name":"Africa/Lagos"}',
  },
  {
    group: 'Build',
    title: 'Venue layouts',
    description: 'Version, edit and publish floor plans.',
    method: 'POST',
    path: '/api/v1/admin/venues/{venue_id}/layout-versions',
    body: '{"name":"Main bowl v1"}',
  },
  {
    group: 'Build',
    title: 'Events',
    description: 'Create events and control their lifecycle.',
    method: 'POST',
    path: '/api/v1/admin/events',
    body: '{"venue_id":"ven_…","name":"Saturday Live","admission_policy":"SINGLE_ENTRY"}',
  },
  {
    group: 'Commerce',
    title: 'Pricing',
    description: 'Define tiers and assign commercial terms.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/price-tiers',
    body: '{"code":"STANDARD","name":"Standard","amount_minor":250000,"currency":"NGN"}',
  },
  {
    group: 'Commerce',
    title: 'Inventory',
    description: 'Materialize the published layout into event inventory.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/materialize-layout',
    body: '{}',
  },
  {
    group: 'Commerce',
    title: 'Blocks',
    description: 'Protect operational inventory and release safely.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/blocks',
    body: '{"purpose":"PRODUCTION","reason":"Sightline hold","reserved_unit_ids":[]}',
  },
  {
    group: 'Commerce',
    title: 'Allocations',
    description: 'Assign neutral channel or non-public inventory.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/allocations',
    body: '{"mode":"CHANNEL","partner_id":"ptr_…","purpose":"CHANNEL","release_destination":{"kind":"SHARED"}}',
  },
  {
    group: 'Partners',
    title: 'Partner configuration',
    description: 'Credentials, access, return destinations and webhooks.',
    method: 'PUT',
    path: '/api/v1/admin/partners/{partner_id}/allowed-return-urls',
    body: '{"urls":["https://partner.example/checkout/return"]}',
  },
  {
    group: 'Live event',
    title: 'Sales lifecycle',
    description: 'Open sales only after the event passes its readiness gate.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/open-sales',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Pause sales',
    description: 'Stop new acquisition while existing protected transactions resolve.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/pause-sales',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Resume sales',
    description: 'Resume acquisition from an explicitly paused event.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/resume-sales',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Close sales',
    description: 'Close acquisition without erasing existing commercial history.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/close-sales',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Cancel event',
    description: 'Cancel with an explicit auditable operational reason.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/cancel',
    body: '{"reason":"Event cancelled by promoter"}',
  },
  {
    group: 'Live event',
    title: 'Complete event',
    description: 'Mark closed event operations complete while retaining all history.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/complete',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Ticket operations',
    description: 'Void, rotate credentials, or explicitly release capacity.',
    method: 'POST',
    path: '/api/v1/admin/tickets/{ticket_id}/void',
    body: '{"reason":"Customer support correction"}',
  },
  {
    group: 'Live event',
    title: 'Admission operations',
    description: 'Supervised overrides and auditable admission reversal.',
    method: 'POST',
    path: '/api/v1/admin/admissions/manual-override',
    body: '{"event_id":"evt_…","ticket_id":"tkt_…","reason":"Accessibility desk verification","supervisor_user_id":"usr_…"}',
  },
  {
    group: 'Operations',
    title: 'Inventory reporting',
    description: 'Reconcile current inventory obligations separately from historical sales.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/reports/inventory',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Commercial reporting',
    description:
      'Inspect immutable Sale facts and current sold capacity without conflating issuance.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/reports/sales',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Admission reporting',
    description: 'Review active and reversed Admissions plus every scan outcome.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/reports/admission',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Audit explorer',
    description: 'Search a bounded, stable page of append-only material state changes.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/audit?limit=50',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Accreditation export',
    description: 'Generate a read-only CSV snapshot without QR credentials or unnecessary PII.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/accreditation-export',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Metrics and alerts',
    description: 'Inspect advisory operational signals derived from authoritative records.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/metrics',
    body: '',
  },
];

export type WorkflowField = {
  key: string;
  name: string;
  label: string;
  source: 'path' | 'body';
  kind: 'text' | 'number' | 'boolean' | 'json';
  required: boolean;
};

export type WorkflowValues = Record<string, string>;

function humanize(value: string) {
  return value.replace(/_/g, ' ').replace(/\b\w/g, (character) => character.toUpperCase());
}

function sampleBody(workflow: Workflow): Record<string, unknown> {
  if (!workflow.body.trim()) return {};

  try {
    const parsed = JSON.parse(workflow.body) as unknown;
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

function bodyKind(value: unknown): WorkflowField['kind'] {
  if (typeof value === 'number') return 'number';
  if (typeof value === 'boolean') return 'boolean';
  if (Array.isArray(value) || (value !== null && typeof value === 'object')) return 'json';
  return 'text';
}

function valueToInput(value: unknown) {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

export function workflowFields(workflow: Workflow): WorkflowField[] {
  const fields: WorkflowField[] = [];
  const pathParameters = [
    ...new Set(Array.from(workflow.path.matchAll(/\{([^}]+)\}/g), (match) => match[1]!)),
  ];

  for (const name of pathParameters) {
    fields.push({
      key: `path:${name}`,
      name,
      label: humanize(name),
      source: 'path',
      kind: 'text',
      required: true,
    });
  }

  for (const [name, value] of Object.entries(sampleBody(workflow))) {
    fields.push({
      key: `body:${name}`,
      name,
      label: humanize(name),
      source: 'body',
      kind: bodyKind(value),
      required: true,
    });
  }

  return fields;
}

export function initialWorkflowValues(workflow: Workflow): WorkflowValues {
  const values: WorkflowValues = {};

  for (const field of workflowFields(workflow)) {
    if (field.source === 'path') {
      values[field.key] = '';
      continue;
    }

    values[field.key] = valueToInput(sampleBody(workflow)[field.name]);
  }

  return values;
}

function materializeBodyValue(field: WorkflowField, value: string): unknown {
  switch (field.kind) {
    case 'number': {
      const parsed = Number(value);
      if (!Number.isFinite(parsed)) {
        throw new Error(`${field.label} must be a valid number.`);
      }
      return parsed;
    }
    case 'boolean':
      return value === 'true';
    case 'json':
      try {
        return JSON.parse(value);
      } catch {
        throw new Error(`${field.label} must contain valid JSON.`);
      }
    default:
      return value;
  }
}

export function materializeWorkflow(workflow: Workflow, values: WorkflowValues) {
  const fields = workflowFields(workflow);
  let path = workflow.path;
  const body: Record<string, unknown> = {};

  for (const field of fields) {
    const value = values[field.key] ?? '';

    if (field.required && !value.trim()) {
      throw new Error(`${field.label} is required.`);
    }

    if (field.source === 'path') {
      path = path.replace(`{${field.name}}`, encodeURIComponent(value.trim()));
    } else {
      body[field.name] = materializeBodyValue(field, value);
    }
  }

  return {
    path,
    body: workflow.method === 'GET' ? undefined : body,
  };
}
