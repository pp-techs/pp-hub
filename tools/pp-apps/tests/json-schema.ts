/** A minimal JSON Schema checker: only the keywords pp-app.schema.json uses. */
type Schema = {
  type?: string;
  required?: string[];
  properties?: Record<string, Schema>;
  additionalProperties?: boolean;
  items?: Schema;
  pattern?: string;
  minLength?: number;
  minimum?: number;
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
    if (schema.minLength !== undefined && value.length < schema.minLength)
      out.push(`${at}: too short`);
  }
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
      if (sub) out.push(...validate(sub, v, `${at}.${k}`));
      else if (schema.additionalProperties === false) out.push(`${at}: unknown field ${k}`);
    }
  }
  if (Array.isArray(value) && schema.items)
    value.forEach((v, i) => out.push(...validate(schema.items!, v, `${at}[${i}]`)));
  return out;
}
