import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { acceptsAnother, compatible } from './connections';
import type { Port } from './types';

type Fixtures = {
  connection: { name: string; output: Port; input: Port; connectable: boolean }[];
  arity: { name: string; multiple: boolean; bound: number; connectable: boolean }[];
};

// The same file the Go platform reads in sdk/go/plugin/connect_test.go.
const fixtures: Fixtures = JSON.parse(
  readFileSync(resolve(import.meta.dirname, '../../../sdk/contract/port-compatibility.json'), 'utf8'),
);

describe('port compatibility agrees with the platform', () => {
  it('has fixtures to check', () => {
    expect(fixtures.connection.length).toBeGreaterThan(0);
    expect(fixtures.arity.length).toBeGreaterThan(0);
  });

  it.each(fixtures.connection)('$name', ({ output, input, connectable }) => {
    expect(compatible(output, input)).toBe(connectable);
  });

  it.each(fixtures.arity)('$name', ({ multiple, bound, connectable }) => {
    expect(acceptsAnother({ multiple } as Port, bound)).toBe(connectable);
  });
});
