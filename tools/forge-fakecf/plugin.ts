// forge-fakecf: a Forge plugin that writes the schema the cloudflare provider's fake API serves
// and validates against. For each operation in OPERATIONS it records the path and query
// parameters and the type of the JSON 200 response, and it records every model those types
// reach. The output, fakecf_gen.json, is data that the hand-written Go in
// plugins/cloudflare-provider/internal/fakecf interprets.
//
// Types are written inline, except that a reference to a model is written as {"k":"ref"} and
// the model is written once under "models". Only models can be recursive, so inlining the rest
// terminates.

import { definePlugin, type Field, type Forge, type Method, type Type } from '@cloudflare/forge/plugin';

import { OPERATIONS } from './operations';

type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };

interface Param {
  name: string;
  type: JsonValue;
}

/** Writes `type`, adding each model it references to `pending`. */
function encode(type: Type, pending: Set<string>, models: Map<string, Type>): JsonValue {
  switch (type.kind) {
    case 'scalar':
      return type.scalar === 'number'
        ? { k: 'scalar', s: 'number', n: type.number }
        : { k: 'scalar', s: type.scalar };
    case 'enum':
      return { k: 'enum', v: [...type.values] };
    case 'array': {
      const out: { [key: string]: JsonValue } = { k: 'array', e: encode(type.elem, pending, models) };
      if (type.minItems !== undefined) out.min = type.minItems;
      if (type.maxItems !== undefined) out.max = type.maxItems;
      return out;
    }
    case 'object': {
      const fields = type.fields.map((field) => encodeField(field, pending, models));
      const out: { [key: string]: JsonValue } = { k: 'object', f: fields };
      if (type.extra !== undefined) out.x = encode(type.extra, pending, models);
      return out;
    }
    case 'union': {
      const out: { [key: string]: JsonValue } = {
        k: 'union',
        m: type.members.map((member) => encode(member, pending, models)),
        excl: type.exclusive,
      };
      if (type.discriminator !== undefined) {
        const cases: { [key: string]: JsonValue } = {};
        for (const [value, member] of type.discriminator.cases) {
          cases[value] = encode(member, pending, models);
        }
        out.d = { w: type.discriminator.wireName, c: cases };
      }
      return out;
    }
    case 'intersection':
      return { k: 'all', m: type.members.map((member) => encode(member, pending, models)) };
    case 'model': {
      const name = type.model.schemaName;
      if (!models.has(name)) {
        models.set(name, type.model.type);
        pending.add(name);
      }
      return { k: 'ref', r: name };
    }
    default: {
      const never: never = type;
      throw new Error(`unhandled type ${JSON.stringify(never)}`);
    }
  }
}

function encodeField(field: Field, pending: Set<string>, models: Map<string, Type>): JsonValue {
  const out: { [key: string]: JsonValue } = { w: field.wireName, t: encode(field.type, pending, models) };
  if (field.required) out.req = true;
  if (field.sensitive) out.sens = true;
  return out;
}

/** The object types a request type is made of: itself, an intersection's members, or a model's. */
function requestObjects(type: Type | undefined, out: Type[] = []): Type[] {
  if (type === undefined) return out;
  switch (type.kind) {
    case 'object':
      out.push(type);
      break;
    case 'intersection':
      for (const member of type.members) requestObjects(member, out);
      break;
    case 'model':
      requestObjects(type.model.type, out);
      break;
    default:
      break;
  }
  return out;
}

function encodeOperation(method: Method, pending: Set<string>, models: Map<string, Type>): JsonValue {
  const pathParams: Param[] = [];
  const queryParams: Param[] = [];
  for (const object of requestObjects(method.request)) {
    if (object.kind !== 'object') continue;
    for (const field of object.fields) {
      const location = method.location(field);
      if (location === undefined) continue;
      const param = { name: field.wireName, type: encode(field.type, pending, models) };
      (location.in === 'path' ? pathParams : queryParams).push(param);
    }
  }
  const ok = method.responses.find((response) => response.status === '200') ?? method.responses[0];
  if (ok === undefined || ok.json === undefined) {
    throw new Error(`${method.httpMethod} ${method.path} has no JSON success response`);
  }
  queryParams.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  return {
    method: method.httpMethod,
    path: method.path,
    pathParams: pathParams.map((p) => ({ name: p.name, t: p.type })),
    queryParams: queryParams.map((p) => ({ name: p.name, t: p.type })),
    response: encode(ok.json.type, pending, models),
  };
}

function buildSchema(forge: Forge) {
  const byKey = new Map<string, Method>();
  for (const group of forge.groups) {
    for (const method of group.methods) {
      const key = `${method.httpMethod} ${method.path}`;
      if (!byKey.has(key)) byKey.set(key, method);
    }
  }
  const missing = OPERATIONS.filter((key) => !byKey.has(key));
  if (missing.length > 0) {
    throw new Error(`operations missing from the spec:\n  ${missing.join('\n  ')}`);
  }

  const models = new Map<string, Type>();
  const pending = new Set<string>();
  const operations = [...OPERATIONS].sort().map((key) => encodeOperation(byKey.get(key)!, pending, models));

  const encodedModels = new Map<string, JsonValue>();
  while (pending.size > 0) {
    const [name] = pending;
    pending.delete(name);
    encodedModels.set(name, encode(models.get(name)!, pending, models));
  }
  const modelsOut: { [key: string]: JsonValue } = {};
  for (const name of [...encodedModels.keys()].sort()) {
    modelsOut[name] = encodedModels.get(name)!;
  }

  const out = { specVersion: forge.version, operations, models: modelsOut };
  return [{ path: 'fakecf_gen.json', content: `${JSON.stringify(out, null, 1)}\n` }];
}

export const { generate, generateGroup } = definePlugin({ generate: buildSchema });
