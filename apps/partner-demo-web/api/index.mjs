/* global URL */
import { listener } from '../server.mjs';

export default async function handler(request, response) {
  const requestURL = new URL(request.url || '/', 'http://internal');
  const forwardedPath = requestURL.searchParams.get('path');
  if (forwardedPath) request.url = forwardedPath;
  return listener(request, response);
}
