// Run: node ui/src/mcp-names.test.mjs   (Node 23.6+ runs the TypeScript file directly)
import assert from "node:assert/strict";
import { normalizeName, splitCommand, suggestName } from "./mcp-names.ts";

const cases = [
  [suggestName("url", "https://mcp.apify.com/?tools=actors", ""), "apify"],
  [suggestName("url", "https://api-jd2resume.theprojectphoenix.top/mcp", ""), "jd2resume"],
  [suggestName("url", "http://localhost:8080/mcp", ""), "localhost"],
  [suggestName("url", "http://127.0.0.1:9/mcp", ""), "local-server"],
  [suggestName("url", "not a url", ""), ""],
  [suggestName("command", "", "npx -y @modelcontextprotocol/server-filesystem C:/x"), "filesystem"],
  [suggestName("command", "", "uvx mcp-server-fetch"), "fetch"],
  [suggestName("command", "", "node C:\\tools\\my-thing.js"), "my-thing"],
  [suggestName("command", "", "docker run -i foo"), "docker"],
  [suggestName("command", "", ""), ""],
  [normalizeName("  My Server!! v2 "), "my-server-v2"],
  [normalizeName("x".repeat(40)), "x".repeat(32)],
];
for (const [got, want] of cases) assert.equal(got, want);
assert.deepEqual(splitCommand("npx -y \"my pkg\" 'a b' x"), ["npx", "-y", "my pkg", "a b", "x"]);
console.log(`ok: ${cases.length + 1} checks`);
