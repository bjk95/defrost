import { defineCollection } from "astro:content";
import { docsSchema } from "@astrojs/starlight/schema";
import { glob } from "astro/loaders";
import { fileURLToPath } from "node:url";

// The public documentation source lives in the repo-level docs/ tree —
// not under this Astro project's src/content/. The glob loader reads
// from there, with `!**/_*/**` ensuring docs/_internal/** is excluded
// at the build-input level. The static-site builder is the gate that
// keeps internal specs private; this is where that promise is enforced.
//
// Path is anchored to this file's URL (not process.cwd()) so the
// loader resolves correctly regardless of where `astro build` is
// invoked from. content.config.ts lives at
// web/docs-site/src/content.config.ts; ../../../docs/ is the repo's
// docs/ directory.
const docsBase = fileURLToPath(new URL("../../../docs/", import.meta.url));

export const collections = {
  docs: defineCollection({
    loader: glob({
      pattern: ["**/*.{md,mdx}", "!**/_*/**"],
      base: docsBase,
    }),
    schema: docsSchema(),
  }),
};
