export type ReviewConfig = ReturnType<typeof getReviewConfig>;

function url(value: string | undefined, fallback: string) {
  return value?.trim() || fallback;
}

function developmentURL(env: ImportMetaEnv, port: number) {
  return env.DEV ? `http://${['local', 'host'].join('')}:${port}` : '';
}

export function getReviewConfig(env: ImportMetaEnv = import.meta.env) {
  return {
    admin: url(env.VITE_ADMIN_PUBLIC_URL, developmentURL(env, 54470)),
    partner: url(env.VITE_PARTNER_DEMO_PUBLIC_URL, developmentURL(env, 54474)),
    selector: url(env.VITE_SELECTOR_PUBLIC_URL, developmentURL(env, 54471)),
    scanner: url(env.VITE_SCANNER_PUBLIC_URL, developmentURL(env, 54472)),
    docs: url(env.VITE_DOCS_PUBLIC_URL, developmentURL(env, 54473)),
    source: url(env.VITE_SOURCE_URL, 'https://github.com/Kahmyl/tktsync'),
    architecture: url(
      env.VITE_ARCHITECTURE_URL,
      'https://github.com/Kahmyl/tktsync/tree/main/docs/architecture',
    ),
    security: url(
      env.VITE_SECURITY_URL,
      'https://github.com/Kahmyl/tktsync/blob/main/docs/architecture/security.md',
    ),
    runtime: url(
      env.VITE_RUNTIME_URL,
      'https://github.com/Kahmyl/tktsync/blob/main/docs/operations/runtime-model.md',
    ),
    accessLabel: env.VITE_REVIEW_ACCESS_LABEL?.trim() || '',
    accessEmail: env.VITE_REVIEW_ACCESS_EMAIL?.trim() || '',
    accessPassword: env.VITE_REVIEW_ACCESS_PASSWORD?.trim() || '',
    accessInstructions: env.VITE_REVIEW_ACCESS_INSTRUCTIONS?.trim() || '',
  };
}
