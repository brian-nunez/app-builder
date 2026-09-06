/**
 * The JSON Schema subset the inspector knows how to render.
 *
 * The Go platform carries schemas as opaque maps and validates them with a full
 * JSON Schema implementation. This type describes only what the editor reads when
 * it decides which control to show, so it is hand-written rather than generated.
 */
export type Schema = {
  type?: string;
  title?: string;
  description?: string;
  writeOnly?: boolean;
  default?: unknown;
  enum?: string[];
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  format?: string;
  items?: Schema;
  minItems?: number;
  uniqueItems?: boolean;
  properties?: Record<string, Schema>;
  required?: string[];
};
