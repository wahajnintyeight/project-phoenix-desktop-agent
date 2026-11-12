// Run: node ui/src/choice-parser.test.mjs
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import ts from "typescript";

const source = await readFile(new URL("./choice-parser.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { splitChoices } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);

const question = splitChoices("Choose a board.\n```choices\nWhich board?\n- LinkedIn\n- Indeed\n```");
assert.equal(question.body, "Choose a board.");
assert.deepEqual(question.choices, [{ question: "Which board?", options: ["LinkedIn", "Indeed"], kind: "question" }]);

const companies = splitChoices("Found five companies.\n```actions\nWhich company?\n- Explore Nextcloud\n- Explore PROBIS\n- Explore Comeen\n- Explore byteNative\n- Explore Fronterio\n```");
assert.equal(companies.body, "Found five companies.");
assert.deepEqual(companies.choices[0], { question: "Which company?", options: ["Explore Nextcloud", "Explore PROBIS", "Explore Comeen", "Explore byteNative", "Explore Fronterio"], kind: "action" });
assert.deepEqual(splitChoices("Found companies.\n```actions\nWhich company?\n- Explore Nextcloud"), { body: "Found companies.", choices: [] });

const suggestions = splitChoices("Finished.\n```suggestions\nSuggested next steps\n- Review results\n- Start another search\n```");
assert.equal(suggestions.body, "Finished.");
assert.deepEqual(suggestions.choices, []);
assert.deepEqual(splitChoices("Still writing.\n```choices\nNext\n- One"), { body: "Still writing.", choices: [] });
assert.equal(splitChoices("Text\n```choices\nNo options\n```").body, "Text\n```choices\nNo options\n```");
const many = splitChoices("```choices\nPick\n" + Array.from({ length: 12 }, (_, i) => `- Option ${i}`).join("\n") + "\n```");
assert.equal(many.choices[0]?.options.length, 8);
const two = splitChoices("Questions:\n```choices\nWhich board?\n- LinkedIn\n- Indeed\n```\n```choices\nCV?\n- Upload\n- Already in files\n```");
assert.equal(two.body, "Questions:");
assert.equal(two.choices.length, 2);
assert.equal(two.choices[1]?.question, "CV?");
const streaming = splitChoices("Questions:\n```choices\nWhich board?\n- Indeed\n```\n```choices\nCV?\n- Upload");
assert.equal(streaming.body, "Questions:");
assert.equal(streaming.choices.length, 1);
const limited = splitChoices(Array.from({ length: 5 }, (_, i) => `\`\`\`choices\nQuestion ${i}?\n- Yes\n\`\`\``).join("\n"));
assert.equal(limited.choices.length, 4);
assert(limited.body.includes("Question 4?"));
console.log("Streaming question menu parsing checks passed.");
