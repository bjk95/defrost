// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import react from "@astrojs/react";
import tailwindcss from "@tailwindcss/vite";

// Builds the public documentation site. Starlight handles MDX, the
// sidebar, full-text search, prev/next nav, the dark-mode toggle, and
// "edit on GitHub". The content collection (src/content.config.ts)
// reads markdown straight from the repo's docs/ tree, with
// docs/_internal/** explicitly excluded so internal specs cannot leak.
//
// Tailwind v4 is wired through @tailwindcss/vite. The
// @astrojs/starlight-tailwind compatibility plugin (imported in
// src/styles/global.css) ensures our Tailwind utilities and Starlight's
// own styles coexist via CSS layers.
//
// React integration is on so we can drop interactive shadcn islands
// from web/src/components/ui/ into MDX where useful. Pure-prose pages
// ship zero JS.
export default defineConfig({
  // Project sites on GitHub Pages live under https://<user>.github.io/<repo>/.
  // The DOCS_BASE / DOCS_SITE env vars let CI override these without a
  // code change.
  site: process.env.DOCS_SITE ?? "https://bjk95.github.io",
  base: process.env.DOCS_BASE ?? "/defrost/",
  trailingSlash: "ignore",
  output: "static",
  outDir: "./dist",
  vite: {
    plugins: [tailwindcss()],
  },
  integrations: [
    react(),
    starlight({
      title: "defrost",
      description:
        "Track AI evals, metrics, and tests with Git as the database.",
      customCss: ["./src/styles/global.css"],
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/bjk95/defrost",
        },
      ],
      editLink: {
        baseUrl: "https://github.com/bjk95/defrost/edit/main/",
      },
      // Explicit sidebar so order is intentional and the section index
      // pages are always reachable. Each `slug` is the entry ID inside
      // the docs collection (i.e. the file path relative to docs/,
      // without the .md extension).
      sidebar: [
        {
          label: "Guides",
          items: [
            { slug: "guides", label: "Overview" },
            { slug: "guides/quickstart" },
            { slug: "guides/recording-tests" },
            { slug: "guides/recording-evals" },
            { slug: "guides/suppressing-tests" },
            { slug: "guides/dashboard" },
            { slug: "guides/ci-setup" },
          ],
        },
        {
          label: "Concepts",
          items: [
            { slug: "concepts", label: "Overview" },
            { slug: "concepts/git-as-database" },
            { slug: "concepts/defrost-branch" },
            { slug: "concepts/otel-as-ingestion" },
            { slug: "concepts/suppression" },
          ],
        },
        {
          label: "Reference",
          items: [
            { slug: "reference", label: "Overview" },
            { slug: "reference/configuration" },
            { slug: "reference/storage-layout" },
            { slug: "reference/otel-ingestion" },
            { slug: "reference/serve-api" },
            {
              label: "CLI",
              items: [
                { slug: "reference/cli/exec" },
                { slug: "reference/cli/history" },
                { slug: "reference/cli/suppress" },
                { slug: "reference/cli/drop" },
                { slug: "reference/cli/serve" },
              ],
            },
          ],
        },
      ],
    }),
  ],
});
