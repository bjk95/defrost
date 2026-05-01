import { useEffect, useState, type ReactNode } from "react";
import { Link, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTests } from "@/api";
import { Icon, Logo } from "@/components/Icons";
import { FailureScreen, failureKindFromMessage } from "@/components/EmptyStates";
import { LoadingScreen } from "@/components/LoadingScreen";
import { useSuppressions } from "@/lib/utils";
import { TestsPage } from "@/pages/TestsPage";
import { TestDetailPage } from "@/pages/TestDetailPage";
import { RunsPage } from "@/pages/RunsPage";
import { RunDetailPage } from "@/pages/RunDetailPage";
import { SuppressionsPage } from "@/pages/SuppressionsPage";
import { MetricsPage } from "@/pages/MetricsPage";
import { ManagementPage } from "@/pages/ManagementPage";

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

// Returns true only after `value` has been true for `delayMs` continuous ms.
// Used to suppress the loading screen during sub-300ms warm-clone loads.
function useDelayedTrue(value: boolean, delayMs: number): boolean {
  const [delayed, setDelayed] = useState(false);
  useEffect(() => {
    if (!value) {
      setDelayed(false);
      return;
    }
    const timer = setTimeout(() => setDelayed(true), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);
  return delayed;
}

export default function App() {
  const [theme, setTheme] = useTheme();
  const location = useLocation();
  const onSuppressions = location.pathname.startsWith("/suppressions");
  const onMetrics = location.pathname.startsWith("/metrics");
  const onManagement = location.pathname.startsWith("/management");
  const onRuns =
    location.pathname.startsWith("/runs") || location.pathname === "/run";
  const onTests = !onSuppressions && !onRuns && !onMetrics && !onManagement;

  const { data, isPending, error, refetch } = useQuery({
    queryKey: ["tests"],
    queryFn: getTests,
  });
  const suppressionCount = useSuppressions().count;
  const [offline, setOffline] = useState(false);
  const showBootScreen = useDelayedTrue(isPending && !offline, 300);

  if (error && !offline) {
    const msg = (error as Error).message;
    return (
      <FailureScreen
        kind={failureKindFromMessage(msg)}
        stderr={msg}
        onRetry={() => refetch()}
        onContinueOffline={() => setOffline(true)}
        onShowQuickstart={() => setOffline(true)}
      />
    );
  }

  if (isPending && !offline) {
    return showBootScreen ? <LoadingScreen done={false} /> : null;
  }

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
          <NavLink to="/metrics" active={onMetrics}>Metrics</NavLink>
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
          <NavLink to="/management" active={onManagement}>Management</NavLink>
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
          <Route path="/metrics" element={<MetricsPage />} />
          <Route path="/management" element={<ManagementPage />} />
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
