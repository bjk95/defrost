import type { ComponentType } from "react";

// Each entry in the docs glob is a compiled MDX module: the default
// export is the React component, and any named exports come from
// frontmatter via remark-frontmatter (we don't currently consume them
// — page titles are derived from the first H1 in the rendered output
// or from the file path).
//
// Glob excludes docs/_internal/** so internal specs can never reach
// the public site by accident; the static-site builder (this Vite
// config) is the gate that enforces docs/_internal vs. docs/.
type MdxModule = {
  default: ComponentType;
};

// Exclude docs/_internal/** explicitly so internal specs never reach
// the public site. Vite's import.meta.glob accepts an array of
// patterns; a leading "!" inverts a pattern.
const modules = import.meta.glob<MdxModule>(
  ["/../docs/**/*.md", "!/../docs/_internal/**"],
  { eager: true },
);

export interface DocPage {
  // Route path, beginning with / and never ending in /. The repo's
  // docs/index.md becomes "/", docs/guides/index.md becomes "/guides",
  // docs/guides/quickstart.md becomes "/guides/quickstart".
  path: string;
  // Section the page belongs to: "" (top-level), "guides", "reference",
  // "concepts". Used by Sidebar to group entries.
  section: string;
  // Display title. The first H1 in the source would be more accurate,
  // but parsing it from the compiled component is awkward; we derive
  // a clean title from the file basename instead.
  title: string;
  // Whether the page is the section index (docs/guides/index.md, etc).
  isIndex: boolean;
  // The compiled MDX component.
  Component: ComponentType;
}

function titleFromBasename(basename: string): string {
  const cleaned = basename.replace(/\.md$/, "").replace(/-/g, " ");
  return cleaned.replace(/\b\w/g, (c) => c.toUpperCase());
}

function pageFromKey(key: string, mod: MdxModule): DocPage {
  // key looks like "/../docs/guides/quickstart.md".
  const trimmed = key.replace(/^\/\.\.\//, "/").replace(/^\/docs/, "");
  // trimmed is now "/guides/quickstart.md" or "/index.md".
  const segments = trimmed.split("/").filter(Boolean);
  const last = segments[segments.length - 1];
  const isIndex = last === "index.md";

  let path: string;
  if (isIndex) {
    path = "/" + segments.slice(0, -1).join("/");
    if (path !== "/") path = path.replace(/\/$/, "");
  } else {
    path = "/" + segments.join("/").replace(/\.md$/, "");
  }
  if (path === "") path = "/";

  const section = segments.length > 1 ? segments[0] : "";
  let title: string;
  if (isIndex) {
    title = section ? titleFromBasename(section) : "defrost";
  } else {
    title = titleFromBasename(last);
  }

  return {
    path,
    section,
    title,
    isIndex,
    Component: mod.default,
  };
}

export const pages: DocPage[] = Object.entries(modules)
  .map(([key, mod]) => pageFromKey(key, mod))
  .sort((a, b) => a.path.localeCompare(b.path));

export const pageByPath = new Map(pages.map((p) => [p.path, p]));

// Section ordering controls the sidebar group order. Sections not in
// this list appear after the listed ones in alphabetical order.
const SECTION_ORDER = ["guides", "concepts", "reference"] as const;

export interface SectionGroup {
  section: string; // "" for top-level
  title: string;
  index: DocPage | undefined;
  pages: DocPage[];
}

export const sections: SectionGroup[] = (() => {
  const bySection = new Map<string, DocPage[]>();
  for (const p of pages) {
    if (!bySection.has(p.section)) bySection.set(p.section, []);
    bySection.get(p.section)!.push(p);
  }
  const known = SECTION_ORDER.filter((s) => bySection.has(s));
  const extras = [...bySection.keys()]
    .filter((s) => s !== "" && !SECTION_ORDER.includes(s as never))
    .sort();
  const top = bySection.get("") ?? [];
  const ordered: SectionGroup[] = [];
  if (top.length) {
    const idx = top.find((p) => p.isIndex);
    ordered.push({
      section: "",
      title: "Overview",
      index: idx,
      pages: top.filter((p) => !p.isIndex),
    });
  }
  for (const section of [...known, ...extras]) {
    const ps = bySection.get(section) ?? [];
    const idx = ps.find((p) => p.isIndex);
    ordered.push({
      section,
      title: titleFromBasename(section),
      index: idx,
      pages: ps.filter((p) => !p.isIndex),
    });
  }
  return ordered;
})();
