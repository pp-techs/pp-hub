/** A minimal JSON Schema checker: only the keywords the two pp-app.schema.json files use. */
type Schema = {
  type?: string;
  required?: string[];
  properties?: Record<string, Schema>;
  additionalProperties?: boolean | Schema;
  propertyNames?: Schema;
  items?: Schema;
  minItems?: number;
  maxItems?: number;
  uniqueItems?: boolean;
  pattern?: string;
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  const?: unknown;
  enum?: unknown[];
  not?: Schema;
};

export function validate(schema: Schema, value: unknown, at = "$"): string[] {
  const out: string[] = [];
  const isObj = typeof value === "object" && value !== null && !Array.isArray(value);
  switch (schema.type) {
    case "object":
      if (!isObj) return [`${at}: not an object`];
      break;
    case "array":
      if (!Array.isArray(value)) return [`${at}: not an array`];
      break;
    case "string":
      if (typeof value !== "string") return [`${at}: not a string`];
      break;
    case "integer":
      if (!Number.isInteger(value)) return [`${at}: not an integer`];
      break;
  }
  if (typeof value === "string") {
    if (schema.pattern !== undefined && !new RegExp(schema.pattern).test(value))
      out.push(`${at}: does not match ${schema.pattern}`);
    if (schema.minLength !== undefined && Array.from(value).length < schema.minLength)
      out.push(`${at}: too short`);
    if (schema.maxLength !== undefined && Array.from(value).length > schema.maxLength)
      out.push(`${at}: too long`);
  }
  if ("const" in schema && value !== schema.const) out.push(`${at}: not ${String(schema.const)}`);
  if (typeof value === "number" && schema.minimum !== undefined && value < schema.minimum)
    out.push(`${at}: below minimum`);
  if (schema.enum && !schema.enum.includes(value)) out.push(`${at}: not in enum`);
  if (schema.not && validate(schema.not, value, at).length === 0)
    out.push(`${at}: matches a forbidden schema`);
  if (isObj) {
    const o = value as Record<string, unknown>;
    for (const k of schema.required ?? []) if (!(k in o)) out.push(`${at}: missing ${k}`);
    for (const [k, v] of Object.entries(o)) {
      const sub = schema.properties?.[k];
      const extra = schema.additionalProperties;
      if (schema.propertyNames) out.push(...validate(schema.propertyNames, k, `${at} key ${k}`));
      if (sub) out.push(...validate(sub, v, `${at}.${k}`));
      else if (extra === false) out.push(`${at}: unknown field ${k}`);
      else if (typeof extra === "object") out.push(...validate(extra, v, `${at}.${k}`));
    }
  }
  if (Array.isArray(value)) {
    if (schema.items)
      value.forEach((v, i) => out.push(...validate(schema.items!, v, `${at}[${i}]`)));
    if (schema.minItems !== undefined && value.length < schema.minItems)
      out.push(`${at}: too few items`);
    if (schema.maxItems !== undefined && value.length > schema.maxItems)
      out.push(`${at}: too many items`);
    if (schema.uniqueItems && new Set(value.map((v) => JSON.stringify(v))).size !== value.length)
      out.push(`${at}: duplicate items`);
  }
  return out;
}
