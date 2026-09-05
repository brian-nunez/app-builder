export type Schema = {
  type?: string;
  title?: string;
  writeOnly?: boolean;
  description?: string;
  default?: unknown;
  enum?: string[];
  minimum?: number;
  maximum?: number;
  properties?: Record<string, Schema>;
  required?: string[];
};
export type Port = {
  side?: 'left' | 'right' | 'top' | 'bottom';
  name: string;
  title: string;
  kind: 'data' | 'resource';
  resourceType?: string;
  required?: boolean;
  sensitive?: boolean;
  multiple?: boolean;
  schema: Schema;
};
export type Manifest = {
  protocol: string;
  name: string;
  version: string;
  title: string;
  description: string;
  category: string;
  kind: 'action' | 'resource' | 'trigger';
  configSchema: Schema;
  inputs: Port[];
  outputs: Port[];
  permissions: string[];
  logFields?: string[];
  digest: string;
};
export type WorkflowNode = {
  responsePort?: string;
  id: string;
  plugin: string;
  version: string;
  name: string;
  config: Record<string, unknown>;
  logFields?: string[];
  position: { x: number; y: number };
};
export type WorkflowEdge = {
  id: string;
  source: string;
  sourcePort: string;
  target: string;
  targetPort: string;
};
export type Graph = { nodes: WorkflowNode[]; edges: WorkflowEdge[] };
export type Workflow = {
  id: string;
  name: string;
  head: number;
  published: number | null;
  updatedAt: string;
  graph: Graph;
};
export type Revision = {
  revision: number;
  name: string;
  author: string;
  message: string;
  createdAt: string;
  graph: Graph;
};
export type Run = {
  id: string;
  workflowId: string;
  revision: number;
  status: string;
  attempt: number;
  error: string | null;
  createdAt: string;
  finishedAt: string | null;
};
export type Step = {
  nodeId: string;
  attempt: number;
  status: string;
  error: string | null;
  startedAt: string;
  finishedAt: string | null;
};
