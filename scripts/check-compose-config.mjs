import { execFileSync } from 'node:child_process';

const environment = { ...process.env };
for (const name of [
  'ADMIN_INVITE_REDIRECT_URL',
  'BROWSER_ALLOWED_ORIGINS',
  'DOCS_LOCAL_API_BASE_URL',
  'SELECTOR_BASE_URL',
  'TICKET_QR_PUBLIC_BASE_URL',
  'VITE_API_BASE_URL',
])
  delete environment[name];
Object.assign(environment, {
  API_HOST_PORT: '59480',
  ADMIN_HOST_PORT: '55470',
  SELECTOR_HOST_PORT: '55471',
  SCANNER_HOST_PORT: '55472',
  DOCS_HOST_PORT: '55473',
  POSTGRES_PORT: '56439',
});

const config = JSON.parse(
  execFileSync('docker', ['compose', '--env-file', '/dev/null', 'config', '--format', 'json'], {
    encoding: 'utf8',
    env: environment,
  }),
);
const expectEqual = (actual, expected, label) => {
  if (actual !== expected) throw new Error(`${label}: got ${actual}, want ${expected}`);
};
const publishedPort = (service) => String(config.services[service].ports[0].published);

expectEqual(publishedPort('api'), '59480', 'API published port');
expectEqual(publishedPort('admin'), '55470', 'Admin published port');
expectEqual(publishedPort('selector'), '55471', 'Selector published port');
expectEqual(publishedPort('scanner'), '55472', 'Scanner published port');
expectEqual(publishedPort('docs'), '55473', 'Docs published port');
expectEqual(publishedPort('postgres'), '56439', 'PostgreSQL published port');
expectEqual(config.services.api.ports[0].target, 8080, 'API container port');
expectEqual(config.services.api.environment.API_PORT, '8080', 'API listen port');
expectEqual(
  config.services.api.environment.TICKET_QR_PUBLIC_BASE_URL,
  'http://localhost:59480',
  'hosted QR base URL',
);
expectEqual(
  config.services.api.environment.SELECTOR_BASE_URL,
  'http://localhost:55471/s',
  'Selector base URL',
);
expectEqual(
  config.services.api.environment.BROWSER_ALLOWED_ORIGINS,
  'http://localhost:55470,http://localhost:55471,http://localhost:55472',
  'browser origins',
);
expectEqual(
  config.services.api.environment.ADMIN_INVITE_REDIRECT_URL,
  'http://localhost:55470/set-password',
  'Admin invite URL',
);
for (const service of ['admin', 'selector', 'scanner', 'docs'])
  expectEqual(
    config.services[service].build.args.VITE_API_BASE_URL,
    'http://localhost:59480',
    `${service} API build URL`,
  );
expectEqual(
  config.services.docs.build.args.VITE_DOCS_LOCAL_API_BASE_URL,
  'http://localhost:59480',
  'Docs local API URL',
);
for (const service of ['api', 'worker', 'seed'])
  expectEqual(
    config.services[service].environment.DATABASE_URL,
    'postgres://tktsync:tktsync@postgres:5432/tktsync?sslmode=disable',
    `${service} database URL`,
  );

console.log('Compose host-port and internal dependency contract passed.');
