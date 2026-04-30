import { useEffect, useState, type ReactNode } from "react";
import { Link, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTests } from "@/api";
import { Icon, Logo } from "@/components/Icons";
import { useSuppressions } from "@/lib/suppressions";
import { TestsPage } from "@/pages/TestsPage";
import { TestDetailPage } from "@/pages/TestDetailPage";
import { RunsPage } from "@/pages/RunsPage";
import { RunDetailPage } from "@/pages/RunDetailPage";
import { SuppressionsPage } from "@/pages/SuppressionsPage";

const THEME_KEY = "defrost.theme.v1";

function useTheme(): [string, (next: string) => void] {
  const [theme, setTheme] = useState<string>(() => {
    if (typeof localStorage === "undefined") return "light";
    return localStorage.getItem(THEME_KEY) || "light";
  });
  useEffect(() => {
    const root = document.documentElement;
    if (theme === "dark") root.classList.add("dark");
    else root.classList.remove("dark");
    try { localStorage.setItem(THEME_KEY, theme); } catch { /* ignored */ }
  }, [theme]);
  return [theme, setTheme];
}

export default function App() {
  const [theme, setTheme] = useTheme();
  const location = useLocation();
  const onSuppressions = location.pathname.startsWith("/suppressions");
  const onRuns =
    location.pathname.startsWith("/runs") || location.pathname === "/run";
  const onTests = !onSuppressions && !onRuns;

  const { data } = useQuery({ queryKey: ["tests"], queryFn: getTests });
  const { count: suppressionCount } = useSuppressions();

  return (
    <div
      style={{
        minHeight: "100vh",
        background: "var(--bg)",
        color: "var(--fg)",
        fontFamily: "var(--font-sans)",
      }}
    >
      <header
        style={{
          display: "flex",
          alignItems: "center",
          gap: 16,
          padding: "12px 24px",
          borderBottom: "1px solid var(--border)",
          position: "sticky",
          top: 0,
          background: "var(--bg)",
          zIndex: 10,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <Logo size={20} />
          <span
            style={{
              fontWeight: 600,
              letterSpacing: "-0.02em",
              fontSize: 14,
            }}
          >
            defrost
          </span>
        </div>
        <nav style={{ display: "flex", gap: 2, marginLeft: 12 }}>
          <NavLink to="/" active={onTests}>Tests</NavLink>
          <NavLink to="/runs" active={onRuns}>Runs</NavLink>
          <NavLink to="/suppressions" active={onSuppressions}>
            <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
              Suppressions
              {suppressionCount > 0 && (
                <span
                  style={{
                    display: "inline-flex",
                    alignItems: "center",
                    justifyContent: "center",
                    minWidth: 18,
                    height: 18,
                    padding: "0 5px",
                    fontSize: 10,
                    fontWeight: 500,
                    fontFamily: "var(--font-mono)",
                    lineHeight: 1,
                    background: "var(--bg-muted)",
                    color: "var(--fg-muted)",
                    border: "1px solid var(--border)",
                    borderRadius: 999,
                  }}
                >
                  {suppressionCount}
                </span>
              )}
            </span>
          </NavLink>
        </nav>
        <div style={{ flex: 1 }} />
        {data && (
          <span
            style={{
              fontSize: 12,
              color: "var(--fg-muted)",
              fontFamily: "var(--font-mono)",
            }}
          >
            {data.runs.length} runs · {data.tests.length} tests
          </span>
        )}
        <button
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          aria-label="Toggle theme"
          style={{
            height: 28,
            width: 28,
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            border: "1px solid var(--border)",
            background: "var(--bg)",
            borderRadius: 6,
            color: "var(--fg-muted)",
            cursor: "pointer",
          }}
        >
          {theme === "dark" ? <Icon.Sun /> : <Icon.Moon />}
        </button>
      </header>

      <main
        style={{
          maxWidth: 1280,
          margin: "0 auto",
          padding: "28px 24px",
          width: "100%",
        }}
      >
        <Routes>
          <Route path="/" element={<TestsPage />} />
          <Route path="/tests" element={<TestsPage />} />
          <Route path="/test" element={<TestDetailPage />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/run" element={<RunDetailPage />} />
          <Route path="/suppressions" element={<SuppressionsPage />} />
        </Routes>
      </main>
    </div>
  );
}

function NavLink({
  active,
  to,
  children,
}: {
  active: boolean;
  to: string;
  children: ReactNode;
}) {
  return (
    <Link
      to={to}
      style={{
        padding: "6px 12px",
        fontSize: 13,
        fontWeight: 500,
        background: active ? "var(--bg-muted)" : "transparent",
        color: active ? "var(--fg)" : "var(--fg-muted)",
        borderRadius: 6,
        textDecoration: "none",
        transition: "all var(--dur-fast) var(--ease-out)",
      }}
    >
      {children}
    </Link>
  );
}
