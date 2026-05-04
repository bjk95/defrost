import type { ReactNode } from "react";
import { Link } from "react-router-dom";

import { Sidebar } from "./Sidebar";

export function DocsLayout({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-30 border-b border-border bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-14 w-full max-w-screen-xl items-center gap-6 px-6">
          <Link
            to="/"
            className="font-mono text-sm font-semibold tracking-tight"
          >
            defrost
          </Link>
          <nav className="ml-auto flex items-center gap-4 text-sm text-fg-muted">
            <a
              href="https://github.com/bjk95/defrost"
              target="_blank"
              rel="noreferrer"
              className="hover:text-foreground"
            >
              GitHub
            </a>
          </nav>
        </div>
      </header>

      <div className="mx-auto flex w-full max-w-screen-xl gap-8 px-6">
        <aside className="sticky top-14 hidden h-[calc(100vh-3.5rem)] w-56 shrink-0 overflow-y-auto md:block">
          <Sidebar />
        </aside>
        <main className="min-w-0 flex-1 py-10">
          <article className="docs-prose mx-auto max-w-3xl">
            {children}
          </article>
        </main>
      </div>
    </div>
  );
}
