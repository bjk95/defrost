import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState, type ReactNode } from "react";
import {
  FailureScreen,
  MetricsEmpty,
  RunsEmpty,
  TestsEmpty,
} from "./EmptyStates";

const meta = {
  title: "States/EmptyStates",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;

// In-app empty states render inside the dashboard chrome (header + nav).
// The chrome below mirrors `<App>` so the stories show what users actually
// see — there's no point demoing an empty state without the surrounding
// frame, the pixel margins matter.

function DashboardChrome({
  active,
  children,
}: {
  active: "tests" | "runs" | "metrics" | "suppressions";
  children: ReactNode;
}) {
  const tabs: ("tests" | "runs" | "metrics" | "suppressions")[] = [
    "tests",
    "runs",
    "metrics",
    "suppressions",
  ];
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
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <svg width={20} height={20} viewBox="0 0 100 100" fill="none">
            <g stroke="currentColor" strokeWidth="8" strokeLinecap="round">
              <line x1="50" y1="20" x2="50" y2="80" />
              <line x1="24" y1="35" x2="76" y2="65" />
              <line x1="24" y1="65" x2="76" y2="35" />
            </g>
            <g fill="currentColor">
              <circle cx="50" cy="20" r="9" />
              <circle cx="50" cy="80" r="9" />
              <circle cx="24" cy="35" r="9" />
              <circle cx="76" cy="35" r="9" />
              <circle cx="24" cy="65" r="9" />
              <circle cx="76" cy="65" r="9" />
              <circle cx="50" cy="50" r="10" />
            </g>
          </svg>
          <span
            style={{ fontWeight: 600, letterSpacing: "-0.02em", fontSize: 14 }}
          >
            defrost
          </span>
        </div>
        <nav style={{ display: "flex", gap: 2, marginLeft: 12 }}>
          {tabs.map((t) => (
            <span
              key={t}
              style={{
                padding: "6px 12px",
                fontSize: 13,
                fontWeight: 500,
                background: t === active ? "var(--bg-muted)" : "transparent",
                color: t === active ? "var(--fg)" : "var(--fg-muted)",
                borderRadius: 6,
                textTransform: "capitalize",
              }}
            >
              {t}
            </span>
          ))}
        </nav>
        <div style={{ flex: 1 }} />
        <span
          style={{
            fontSize: 12,
            color: "var(--fg-muted)",
            fontFamily: "var(--font-mono)",
          }}
        >
          0 runs · 0 tests
        </span>
      </header>
      <main
        style={{
          maxWidth: 1280,
          margin: "0 auto",
          padding: "28px 24px",
          width: "100%",
        }}
      >
        {children}
      </main>
    </div>
  );
}

type Story = StoryObj;

export const TestsNoRuns: Story = {
  name: "Tests · no runs yet",
  render: () => (
    <DashboardChrome active="tests">
      <TestsEmpty />
    </DashboardChrome>
  ),
};

export const RunsNoRuns: Story = {
  name: "Runs · no runs yet",
  render: () => (
    <DashboardChrome active="runs">
      <RunsEmpty />
    </DashboardChrome>
  ),
};

export const MetricsNoEmissions: Story = {
  name: "Metrics · OTel not wired",
  render: () => (
    <DashboardChrome active="metrics">
      <MetricsEmpty />
    </DashboardChrome>
  ),
};

// Boot failures replace the dashboard entirely — no chrome, full-bleed dark
// terminal styling.

export const FailureCloneFailed: Story = {
  name: "Boot failure · clone failed",
  parameters: {
    docs: {
      description: {
        story:
          "Triggered when `git clone --branch _defrost` returns non-zero (network, missing origin, push rejected). Defaults to a network-timeout stderr.",
      },
    },
  },
  render: () => {
    const [count, setCount] = useState(0);
    return (
      <FailureScreen
        kind="clone-failed"
        onRetry={() => setCount((n) => n + 1)}
        onContinueOffline={() => alert("Continue with empty history")}
        stderr={
          count === 0
            ? undefined
            : `attempt ${count + 1}: still failing\nfatal: unable to access 'https://github.com/you/your-repo/': Operation timed out`
        }
      />
    );
  },
};

export const FailureAuthRequired: Story = {
  name: "Boot failure · auth required",
  parameters: {
    docs: {
      description: {
        story:
          "Stderr contains `Authentication failed`, `could not read Username`, or `Repository not found`. Adds an inline `GITHUB_TOKEN` snippet.",
      },
    },
  },
  render: () => (
    <FailureScreen
      kind="auth-required"
      onRetry={() => alert("retry")}
      onContinueOffline={() => alert("offline")}
    />
  ),
};

export const FailureEmptyRepo: Story = {
  name: "Boot failure · first run",
  parameters: {
    docs: {
      description: {
        story:
          "Clone succeeded but the `_defrost` branch doesn't exist yet. Uses the connected/positive accent rather than danger.",
      },
    },
  },
  render: () => (
    <FailureScreen
      kind="empty-repo"
      onShowQuickstart={() => alert("show quickstart")}
      onRetry={() => alert("retry")}
    />
  ),
};
