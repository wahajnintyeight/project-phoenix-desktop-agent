import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import ts from "typescript";

const source = await readFile(new URL("./mention-query.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { mentionQuery } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);

assert.deepEqual(mentionQuery("@", 1, 1), { start: 0, query: "" });
assert.deepEqual(mentionQuery("Use @annual report please", 18, 18), { start: 4, query: "annual report" });
assert.deepEqual(mentionQuery("Look (@file", 11, 11), { start: 6, query: "file" });
assert.equal(mentionQuery("person@example.com", 18, 18), null);
assert.equal(mentionQuery("@file", 5, 3), null);
