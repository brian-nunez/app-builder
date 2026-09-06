import { describe, expect, it } from 'vitest';
import { COMPONENTS, WORKFLOWS, href, parse } from './route';

describe('routes', () => {
  it.each([
    ['/', WORKFLOWS],
    ['', WORKFLOWS],
    ['/components', COMPONENTS],
    ['/workflows/abc-123', { name: 'editor', workflowId: 'abc-123' }],
    ['/workflows/abc-123/', { name: 'editor', workflowId: 'abc-123' }],
    ['/nonsense', WORKFLOWS],
    ['/workflows', WORKFLOWS],
  ])('parses %s', (path, expected) => {
    expect(parse(path)).toEqual(expected);
  });

  it.each([WORKFLOWS, COMPONENTS, { name: 'editor', workflowId: 'w 1' } as const])(
    'round-trips %o',
    (route) => {
      expect(parse(href(route))).toEqual(route);
    },
  );
});
