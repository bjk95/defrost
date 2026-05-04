import { NavLink } from "react-router-dom";

import { cn } from "@/lib/utils";
import { sections } from "./pages";

export function Sidebar() {
  return (
    <nav className="flex flex-col gap-6 py-6 pr-4 text-sm">
      {sections.map((group) => (
        <div key={group.section || "_top"} className="flex flex-col gap-1">
          <div className="px-2 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
            {group.title}
          </div>
          {group.index ? (
            <SidebarLink to={group.index.path} label={group.index.title} />
          ) : null}
          {group.pages.map((p) => (
            <SidebarLink key={p.path} to={p.path} label={p.title} />
          ))}
        </div>
      ))}
    </nav>
  );
}

function SidebarLink({ to, label }: { to: string; label: string }) {
  return (
    <NavLink
      to={to}
      end
      className={({ isActive }) =>
        cn(
          "rounded-md px-2 py-1.5 transition-colors hover:bg-bg-muted hover:text-foreground",
          isActive
            ? "bg-bg-muted font-medium text-foreground"
            : "text-fg-muted",
        )
      }
    >
      {label}
    </NavLink>
  );
}
