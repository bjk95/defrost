// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import react from "@astrojs/react";

// Builds the public documentation site. Starlight handles MDX, the
// sidebar, full-text search, prev/next nav, the dark-mode toggle, and
// "edit on GitHub". The content collection (src/content.config.ts)
// reads markdown straight from the repo's docs/ tree, with
// docs/_internal/** explicitly excluded so internal specs cannot leak.
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
  integrations: [
    react(),
    starlight({
      title: "defrost",
      description:
        "Track AI evals, metrics, and tests with Git as the database.",
      customCss: ["./src/styles/theme.css"],
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
      // Sidebar mirrors the on-disk docs/ layout. Each section's index.md
      // appears as the group landing page.
      sidebar: [
        {
          label: "Guides",
          autogenerate: { directory: "guides" },
        },
        {
          label: "Concepts",
          autogenerate: { directory: "concepts" },
        },
        {
          label: "Reference",
          autogenerate: { directory: "reference" },
        },
      ],
    }),
  ],
});
