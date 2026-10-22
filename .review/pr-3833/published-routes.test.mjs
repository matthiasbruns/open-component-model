import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import {
  baseTag,
  collectAliases,
  gitShow,
  pageUrl,
  parseFrontMatter,
  slugify,
  walkMd,
} from "../../.github/scripts/check-website-redirects.js";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const docsDir = join(root, "website/content/docs");
const prefix = "website/content/docs/";

function route(path, frontMatter) {
  const relative = path.replace(prefix, "");
  if (!relative.endsWith("/_index.md") && relative !== "_index.md") {
    return pageUrl(relative, frontMatter);
  }
  const section = dirname(relative)
    .split("/")
    .filter((part) => part !== ".")
    .map(slugify)
    .join("/");
  return `/docs/${section ? `${section}/` : ""}`;
}

test("all published docs routes remain canonical or have an alias", () => {
  const tag = baseTag(root);
  const previous = execFileSync(
    "git",
    ["ls-tree", "-r", "--name-only", tag, "--", prefix],
    { cwd: root, encoding: "utf8" },
  )
    .trim()
    .split("\n")
    .filter((path) => path.endsWith(".md"));

  const covered = collectAliases(docsDir);
  for (const path of walkMd(docsDir)) {
    const relative = path.slice(root.length + 1);
    covered.add(route(relative, parseFrontMatter(readFileSync(path, "utf8"))));
  }

  const missing = previous
    .map((path) => route(path, parseFrontMatter(gitShow(root, tag, path))))
    .filter((url) => !covered.has(url))
    .sort();

  assert.deepEqual(missing, [], `Published URLs lost by the current docs tree: ${missing.join(", ")}`);
});
