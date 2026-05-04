import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import mdx from "@mdx-js/rollup";
import remarkFrontmatter from "remark-frontmatter";
import remarkGfm from "remark-gfm";
import rehypeSlug from "rehype-slug";
import rehypeAutolinkHeadings from "rehype-autolink-headings";
import rehypeShiki from "@shikijs/rehype";
import path from "node:path";

// Builds the static documentation site. The dashboard SPA is built by
// vite.config.ts; this config is invoked via `npm run build:docs` and
// writes to web/dist-docs/, which the GH Pages workflow uploads.
//
// The docs source lives in web/src/docs-app/. Markdown content lives in
// the repo-level docs/ tree; the Sidebar component imports it via
// import.meta.glob at build time. docs/_internal/ is private and is
// excluded from the glob so internal specs can never leak into the
// public site.
export default defineConfig({
  base: process.env.DOCS_BASE ?? "/",
  plugins: [
    {
      enforce: "pre",
      ...mdx({
        remarkPlugins: [remarkFrontmatter, remarkGfm],
        rehypePlugins: [
          rehypeSlug,
          [
            rehypeAutolinkHeadings,
            { behavior: "wrap", properties: { className: ["heading-anchor"] } },
          ],
          [rehypeShiki, { themes: { light: "github-light", dark: "github-dark" } }],
        ],
        providerImportSource: "@mdx-js/react",
      }),
    },
    react({ include: /\.(jsx|tsx|md|mdx)$/ }),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
      "@docs": path.resolve(__dirname, "../docs"),
      // The Markdown sources live outside web/, so MDX-emitted imports
      // of react/jsx-runtime can't reach web/node_modules through
      // normal resolution. Pin them here.
      react: path.resolve(__dirname, "node_modules/react"),
      "react-dom": path.resolve(__dirname, "node_modules/react-dom"),
      "@mdx-js/react": path.resolve(__dirname, "node_modules/@mdx-js/react"),
    },
  },
  build: {
    outDir: "dist-docs",
    emptyOutDir: true,
    rollupOptions: {
      input: path.resolve(__dirname, "docs.html"),
    },
  },
});
