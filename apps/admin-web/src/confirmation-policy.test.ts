import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return /\.[cm]?[jt]sx?$/.test(entry.name) && !entry.name.endsWith('.test.ts') ? [path] : [];
  });
}

describe('confirmation policy', () => {
  it('uses application dialogs instead of native browser prompts', () => {
    const nativePrompt = /\b(?:window\.)?(?:confirm|alert|prompt)\s*\(/;
    const violations = sourceFiles(import.meta.dirname)
      .filter((path) => nativePrompt.test(readFileSync(path, 'utf8')))
      .map((path) => path.slice(import.meta.dirname.length + 1));

    expect(violations).toEqual([]);
  });
});
