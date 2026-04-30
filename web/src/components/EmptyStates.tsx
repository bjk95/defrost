import { useState, type ReactNode } from "react";
import { Logo } from "./Icons";

export type FailureKind = "clone-failed" | "auth-required" | "empty-repo";

const FAILURE_COPY: Record<
  FailureKind,
  {
    badge: string;
    headline: string;
    sub: string;
    primary: { label: string; action: "retry" | "offline" | "quickstart" };
    secondary: { label: string; action: "retry" | "offline" | "quickstart" };
    hint: string | null;
  }
> = {
  "clone-failed": {
    badge: "Boot failed",
    headline: "Couldn't clone _defrost branch",
    sub: "Your Git remote rejected the request. The dashboard needs read access to the _defrost branch on origin to load history.",
    primary: { label: "Retry", action: "retry" },
    secondary: { label: "Continue with empty history", action: "offline" },
    hint: "Most often this is a missing or expired credential. Check that `git fetch origin _defrost` works from this directory.",
  },
  "auth-required": {
    badge: "Auth required",
    headline: "Git couldn't authenticate to origin",
    sub: "defrost needs read access to your repo's _defrost branch. Set a GITHUB_TOKEN with repo scope, then retry.",
    primary: { label: "Retry", action: "retry" },
    secondary: { label: "Continue with empty history", action: "offline" },
    hint: null,
  },
  "empty-repo": {
    badge: "First run",
    headline: "No history recorded yet",
    sub: "We connected to origin, but the _defrost branch doesn't exist. It's created automatically the first time you run a test through defrost exec.",
    primary: { label: "Show quickstart", action: "quickstart" },
    secondary: { label: "Refresh", action: "retry" },
    hint: null,
  },
};

const DEFAULT_STDERR: Record<FailureKind, string> = {
  "clone-failed": `fatal: unable to access 'https://github.com/you/your-repo/':
  Failed to connect to github.com port 443: Operation timed out
hint: Check your network connection or proxy settings.`,
  "auth-required": `Cloning into '/tmp/defrost-load-3819'...
remote: Repository not found.
fatal: Authentication failed for 'https://github.com/you/your-repo/'`,
  "empty-repo": `From https://github.com/you/your-repo
 * [new branch]      main -> origin/main
warning: remote branch '_defrost' not found in upstream origin`,
};

// Detects which FailureScreen variant best matches a server error message.
// The server returns `{error: msg}` strings from git directly; we keyword-
// match the common phrases.
export function failureKindFromMessage(msg: string): FailureKind {
  const m = msg.toLowerCase();
  if (
    m.includes("authentication failed") ||
    m.includes("could not read username") ||
    m.includes("could not read password") ||
    m.includes("repository not found")
  )
    return "auth-required";
  if (
    m.includes("remote branch") &&
    m.includes("_defrost") &&
    m.includes("not found")
  )
    return "empty-repo";
  return "clone-failed";
}

function CodeBlock({
  children,
  copyable = true,
  dim = false,
}: {
  children: string;
  copyable?: boolean;
  dim?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <div
      style={{
        position: "relative",
        background: dim ? "oklch(1 0 0 / 0.04)" : "var(--bg-muted)",
        border: dim ? "1px solid oklch(1 0 0 / 0.08)" : "1px solid var(--border)",
        borderRadius: 8,
        padding: "12px 14px",
        fontFamily: "var(--font-mono)",
        fontSize: 12.5,
        lineHeight: 1.6,
        color: dim ? "oklch(1 0 0 / 0.85)" : "var(--fg)",
        whiteSpace: "pre",
        overflowX: "auto",
      }}
    >
      {children}
      {copyable && (
        <button
          onClick={() => {
            navigator.clipboard?.writeText(children);
            setCopied(true);
            setTimeout(() => setCopied(false), 1400);
          }}
          style={{
            position: "absolute",
            top: 8,
            right: 8,
            padding: "3px 8px",
            fontSize: 10.5,
            fontFamily: "var(--font-mono)",
            letterSpacing: 0.04,
            border: dim ? "1px solid oklch(1 0 0 / 0.12)" : "1px solid var(--border)",
            background: dim ? "oklch(1 0 0 / 0.06)" : "var(--bg)",
            color: dim ? "oklch(1 0 0 / 0.7)" : "var(--fg-muted)",
            borderRadius: 5,
            cursor: "pointer",
          }}
        >
          {copied ? "copied" : "copy"}
        </button>
      )}
    </div>
  );
}

function ErrorIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <path
        d="M8 1.5L15 14H1L8 1.5z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <line
        x1="8"
        y1="6"
        x2="8"
        y2="9.5"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <circle cx="8" cy="11.5" r="0.8" fill="currentColor" />
    </svg>
  );
}

function ConnectedIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <circle cx="8" cy="8" r="6.5" stroke="currentColor" strokeWidth="1.5" />
      <path
        d="M5 8.5l2 2 4-4.5"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

// Full-bleed boot failure screen. Uses fixed dark colors regardless of theme,
// matching the terminal-style loading screen so a failure is a visual mode
// swap, not a chrome change.
export function FailureScreen({
  kind = "clone-failed",
  stderr,
  onRetry,
  onContinueOffline,
  onShowQuickstart,
}: {
  kind?: FailureKind;
  stderr?: string;
  onRetry?: () => void;
  onContinueOffline?: () => void;
  onShowQuickstart?: () => void;
}) {
  const copy = FAILURE_COPY[kind];
  const isEmpty = kind === "empty-repo";
  const accent = isEmpty ? "var(--terminal-fg)" : "var(--danger)";
  const accentSoft = isEmpty
    ? "color-mix(in oklch, var(--terminal-fg) 18%, transparent)"
    : "color-mix(in oklch, var(--danger) 22%, transparent)";

  const handle = (action: "retry" | "offline" | "quickstart") => {
    if (action === "retry") onRetry?.();
    else if (action === "offline") onContinueOffline?.();
    else if (action === "quickstart") onShowQuickstart?.();
  };

  return (
    <div
      style={{
        width: "100%",
        minHeight: "100vh",
        background: "var(--gray-1000)",
        color: "var(--gray-50)",
        fontFamily: "var(--font-mono)",
        display: "flex",
        flexDirection: "column",
        padding: "40px 48px",
        gap: 24,
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 10, color: "var(--gray-50)" }}>
          <Logo size={18} />
          <span
            style={{
              fontFamily: "var(--font-sans)",
              fontWeight: 600,
              fontSize: 14,
              letterSpacing: -0.01,
            }}
          >
            defrost
          </span>
          <span style={{ color: "oklch(1 0 0 / 0.3)", fontSize: 12 }}>·</span>
          <span style={{ color: "oklch(1 0 0 / 0.5)", fontSize: 12 }}>localhost:6969</span>
        </div>
        <span
          style={{
            padding: "3px 9px",
            fontSize: 10.5,
            letterSpacing: 0.06,
            textTransform: "uppercase",
            fontWeight: 600,
            color: accent,
            background: accentSoft,
            border: `1px solid ${accentSoft}`,
            borderRadius: 999,
          }}
        >
          {copy.badge}
        </span>
      </div>

      <div
        style={{
          flex: 1,
          minHeight: 0,
          display: "grid",
          gridTemplateColumns: "1.1fr 1fr",
          gap: 56,
          alignItems: "start",
          paddingTop: 12,
        }}
      >
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 22,
            maxWidth: 460,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 10, color: accent }}>
            {isEmpty ? <ConnectedIcon /> : <ErrorIcon />}
            <span
              style={{
                fontSize: 11,
                letterSpacing: 0.08,
                textTransform: "uppercase",
                fontWeight: 600,
              }}
            >
              {isEmpty ? "Connected" : "Error"}
            </span>
          </div>

          <h1
            style={{
              fontFamily: "var(--font-sans)",
              fontWeight: 600,
              fontSize: 28,
              lineHeight: 1.15,
              letterSpacing: -0.025,
              color: "var(--gray-50)",
              margin: 0,
            }}
          >
            {copy.headline}
          </h1>

          <p
            style={{
              fontFamily: "var(--font-sans)",
              fontSize: 14,
              lineHeight: 1.6,
              color: "oklch(1 0 0 / 0.65)",
              margin: 0,
            }}
          >
            {copy.sub}
          </p>

          {kind === "auth-required" && (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              <span
                style={{
                  fontSize: 11,
                  color: "oklch(1 0 0 / 0.5)",
                  letterSpacing: 0.04,
                  textTransform: "uppercase",
                }}
              >
                set token, then retry
              </span>
              <CodeBlock dim>{`export GITHUB_TOKEN=ghp_...
defrost serve`}</CodeBlock>
            </div>
          )}

          {kind === "empty-repo" && (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              <span
                style={{
                  fontSize: 11,
                  color: "oklch(1 0 0 / 0.5)",
                  letterSpacing: 0.04,
                  textTransform: "uppercase",
                }}
              >
                record your first run
              </span>
              <CodeBlock dim>{`defrost exec go test ./...`}</CodeBlock>
              <p
                style={{
                  fontFamily: "var(--font-sans)",
                  fontSize: 12.5,
                  color: "oklch(1 0 0 / 0.5)",
                  margin: "4px 0 0",
                  lineHeight: 1.5,
                }}
              >
                Works the same with{" "}
                <code
                  style={{
                    background: "oklch(1 0 0 / 0.08)",
                    padding: "1px 5px",
                    borderRadius: 3,
                  }}
                >
                  pytest
                </code>{" "}
                and{" "}
                <code
                  style={{
                    background: "oklch(1 0 0 / 0.08)",
                    padding: "1px 5px",
                    borderRadius: 3,
                  }}
                >
                  jest
                </code>
                .
              </p>
            </div>
          )}

          {copy.hint && (
            <p
              style={{
                fontFamily: "var(--font-sans)",
                fontSize: 12.5,
                lineHeight: 1.55,
                color: "oklch(1 0 0 / 0.45)",
                margin: 0,
              }}
            >
              {copy.hint}
            </p>
          )}

          <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
            <button
              onClick={() => handle(copy.primary.action)}
              style={{
                padding: "0 14px",
                height: 34,
                fontSize: 13,
                fontWeight: 500,
                fontFamily: "var(--font-sans)",
                border: "none",
                background: "var(--gray-50)",
                color: "var(--gray-1000)",
                borderRadius: 6,
                cursor: "pointer",
              }}
            >
              {copy.primary.label}
            </button>
            <button
              onClick={() => handle(copy.secondary.action)}
              style={{
                padding: "0 14px",
                height: 34,
                fontSize: 13,
                fontWeight: 500,
                fontFamily: "var(--font-sans)",
                border: "1px solid oklch(1 0 0 / 0.18)",
                background: "transparent",
                color: "oklch(1 0 0 / 0.85)",
                borderRadius: 6,
                cursor: "pointer",
              }}
            >
              {copy.secondary.label}
            </button>
          </div>
        </div>

        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 10,
            minWidth: 0,
          }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
            }}
          >
            <span
              style={{
                fontSize: 10.5,
                color: "oklch(1 0 0 / 0.4)",
                letterSpacing: 0.08,
                textTransform: "uppercase",
                fontWeight: 600,
              }}
            >
              git stderr
            </span>
            <span style={{ fontSize: 10.5, color: "oklch(1 0 0 / 0.4)" }}>
              {new Date().toISOString().slice(11, 19)}
            </span>
          </div>
          <pre
            style={{
              margin: 0,
              padding: "16px 18px",
              background: "oklch(1 0 0 / 0.03)",
              border: "1px solid oklch(1 0 0 / 0.08)",
              borderLeft: `2px solid ${accent}`,
              borderRadius: 8,
              fontSize: 12,
              lineHeight: 1.65,
              color: "oklch(1 0 0 / 0.7)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
              maxHeight: 320,
              overflow: "auto",
            }}
          >
            {stderr || DEFAULT_STDERR[kind]}
          </pre>
        </div>
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            fontSize: 11,
            color: "oklch(1 0 0 / 0.4)",
          }}
        >
          <span>{isEmpty ? "ready · 0 runs" : "halted"}</span>
          <span>{isEmpty ? "dashboard online" : "press retry to reload"}</span>
        </div>
        <div
          style={{
            height: 2,
            background: "oklch(1 0 0 / 0.08)",
            borderRadius: 999,
            overflow: "hidden",
          }}
        >
          <div
            style={{
              height: "100%",
              width: "100%",
              background: accent,
              opacity: isEmpty ? 1 : 0.55,
            }}
          />
        </div>
      </div>
    </div>
  );
}

function InlineCode({ children }: { children: ReactNode }) {
  return (
    <code
      style={{
        background: "var(--bg-muted)",
        padding: "1px 5px",
        borderRadius: 4,
        fontSize: 12.5,
        fontFamily: "var(--font-mono)",
      }}
    >
      {children}
    </code>
  );
}

function SkeletonRow() {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <div
        style={{
          width: 140,
          height: 8,
          borderRadius: 2,
          background: "var(--bg-muted)",
        }}
      />
      <div style={{ display: "flex", gap: 3 }}>
        {Array.from({ length: 12 }).map((_, i) => (
          <div
            key={i}
            style={{
              width: 11,
              height: 11,
              borderRadius: 2,
              background: "transparent",
              border: "1px dashed var(--border-strong)",
            }}
          />
        ))}
      </div>
    </div>
  );
}

// Empty Tests page — render when /api/tests returns runs:[] && tests:[].
// (Filter-yields-zero rows is handled inline by the toolbar in TestsPage.)
export function TestsEmpty() {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        padding: "80px 24px 60px",
        gap: 24,
        textAlign: "center",
      }}
    >
      <div
        aria-hidden
        style={{ display: "flex", alignItems: "center", gap: 24, opacity: 0.55 }}
      >
        <SkeletonRow />
        <SkeletonRow />
        <SkeletonRow />
      </div>

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 8,
          maxWidth: 460,
        }}
      >
        <h2
          style={{
            fontSize: 22,
            fontWeight: 600,
            letterSpacing: -0.02,
            margin: 0,
          }}
        >
          No tests recorded yet
        </h2>
        <p
          style={{
            fontSize: 14,
            color: "var(--fg-muted)",
            lineHeight: 1.55,
            margin: 0,
          }}
        >
          Run your test suite through <InlineCode>defrost exec</InlineCode> once and every result
          lands here. Each run becomes a column; each test, a row.
        </p>
      </div>

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 12,
          alignItems: "stretch",
          width: "100%",
          maxWidth: 540,
        }}
      >
        <CodeBlock>{`defrost exec go test ./...`}</CodeBlock>
        <details
          style={{ fontSize: 12.5, color: "var(--fg-muted)", textAlign: "left" }}
        >
          <summary
            style={{ cursor: "pointer", padding: "4px 0", userSelect: "none" }}
          >
            Other runners
          </summary>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 8,
              paddingTop: 8,
            }}
          >
            <CodeBlock>{`defrost exec pytest tests/`}</CodeBlock>
            <CodeBlock>{`defrost exec npm test`}</CodeBlock>
          </div>
        </details>
      </div>

      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          fontSize: 12,
          color: "var(--fg-subtle)",
          marginTop: 4,
        }}
      >
        <span
          style={{
            width: 6,
            height: 6,
            borderRadius: 999,
            background: "var(--accent)",
            animation: "defrostPulseDot 1.4s ease-in-out infinite",
          }}
        />
        <span>Watching for new runs · auto-refreshes every 60s</span>
        <style>{`@keyframes defrostPulseDot { 0%,100% { opacity: 1; } 50% { opacity: 0.4; } }`}</style>
      </div>
    </div>
  );
}

// Empty Runs page — chronological timeline metaphor instead of a grid.
export function RunsEmpty() {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        padding: "80px 24px 60px",
        gap: 28,
      }}
    >
      <div
        aria-hidden
        style={{
          position: "relative",
          width: "100%",
          maxWidth: 480,
          height: 64,
          opacity: 0.7,
        }}
      >
        <div
          style={{
            position: "absolute",
            left: 0,
            right: 0,
            top: "50%",
            height: 1,
            background: "var(--border)",
          }}
        />
        {[0, 0.2, 0.4, 0.6, 0.8, 1].map((p, i) => (
          <div
            key={i}
            style={{
              position: "absolute",
              left: `${p * 100}%`,
              top: "50%",
              transform: "translate(-50%, -50%)",
              width: 10,
              height: 10,
              borderRadius: 999,
              background: "var(--bg)",
              border: "1px dashed var(--border-strong)",
            }}
          />
        ))}
        <span
          style={{
            position: "absolute",
            left: 0,
            top: "calc(50% + 14px)",
            fontFamily: "var(--font-mono)",
            fontSize: 10.5,
            color: "var(--fg-subtle)",
          }}
        >
          oldest
        </span>
        <span
          style={{
            position: "absolute",
            right: 0,
            top: "calc(50% + 14px)",
            fontFamily: "var(--font-mono)",
            fontSize: 10.5,
            color: "var(--fg-subtle)",
          }}
        >
          now
        </span>
      </div>

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 8,
          maxWidth: 460,
          textAlign: "center",
        }}
      >
        <h2
          style={{
            fontSize: 22,
            fontWeight: 600,
            letterSpacing: -0.02,
            margin: 0,
          }}
        >
          No runs to show
        </h2>
        <p
          style={{
            fontSize: 14,
            color: "var(--fg-muted)",
            lineHeight: 1.55,
            margin: 0,
          }}
        >
          Each invocation of <InlineCode>defrost exec</InlineCode> creates one run — a commit on the{" "}
          <InlineCode>_defrost</InlineCode> branch with timing, status, and any evals or metrics
          emitted during the run.
        </p>
      </div>

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 8,
          width: "100%",
          maxWidth: 540,
          alignItems: "stretch",
        }}
      >
        <CodeBlock>{`defrost exec go test ./...`}</CodeBlock>
        <p
          style={{
            fontSize: 12,
            color: "var(--fg-subtle)",
            margin: "2px 4px 0",
            textAlign: "center",
          }}
        >
          Or wire it into CI — defrost exec exits non-zero only when something genuinely broke.
        </p>
      </div>
    </div>
  );
}

// Empty Metrics page — render when /api/metrics returns metrics:[].
// Independent of /api/tests being empty: a project may have plenty of test
// runs but never emit OTLP metrics.
export function MetricsEmpty() {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        padding: "72px 24px 48px",
        gap: 28,
      }}
    >
      <div
        aria-hidden
        style={{
          width: "100%",
          maxWidth: 520,
          height: 140,
          position: "relative",
          opacity: 0.85,
        }}
      >
        <svg viewBox="0 0 520 140" width="100%" height="100%">
          {[0.2, 0.5, 0.8].map((y, i) => (
            <line
              key={i}
              x1="0"
              x2="520"
              y1={140 * y}
              y2={140 * y}
              stroke="var(--border)"
              strokeWidth="1"
              strokeDasharray="2 4"
            />
          ))}
          <line
            x1="0"
            x2="520"
            y1="139"
            y2="139"
            stroke="var(--border-strong)"
            strokeWidth="1"
          />
          <text
            x="6"
            y="14"
            fontFamily="var(--font-mono)"
            fontSize="9.5"
            fill="var(--fg-subtle)"
          >
            eval.score
          </text>
          <text
            x="6"
            y="135"
            fontFamily="var(--font-mono)"
            fontSize="9.5"
            fill="var(--fg-subtle)"
          >
            0.00
          </text>
        </svg>
        <span
          style={{
            position: "absolute",
            inset: 0,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontFamily: "var(--font-mono)",
            fontSize: 11,
            color: "var(--fg-subtle)",
            letterSpacing: 0.04,
          }}
        >
          no data points
        </span>
      </div>

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 8,
          maxWidth: 520,
          textAlign: "center",
        }}
      >
        <h2
          style={{
            fontSize: 22,
            fontWeight: 600,
            letterSpacing: -0.02,
            margin: 0,
          }}
        >
          No metrics emitted yet
        </h2>
        <p
          style={{
            fontSize: 14,
            color: "var(--fg-muted)",
            lineHeight: 1.55,
            margin: 0,
          }}
        >
          Push eval scores, latency, accuracy, or any other gauge through your existing
          OpenTelemetry SDK. defrost exec sets the OTLP endpoint automatically — no client library
          to install.
        </p>
      </div>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: 12,
          width: "100%",
          maxWidth: 720,
        }}
      >
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10.5,
              color: "var(--fg-subtle)",
              letterSpacing: 0.04,
              textTransform: "uppercase",
            }}
          >
            python
          </span>
          <CodeBlock>{`gauge = meter.create_gauge("eval.score")
gauge.set(0.87, {"model": "claude-opus-4-7"})`}</CodeBlock>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10.5,
              color: "var(--fg-subtle)",
              letterSpacing: 0.04,
              textTransform: "uppercase",
            }}
          >
            go
          </span>
          <CodeBlock>{`gauge, _ := meter.Float64Gauge("eval.score")
gauge.Record(ctx, 0.87,
  metric.WithAttributes(attr.String("model","opus")))`}</CodeBlock>
        </div>
      </div>

      <a
        href="https://opentelemetry.io/docs/languages/"
        target="_blank"
        rel="noreferrer"
        style={{
          fontSize: 12.5,
          color: "var(--fg-muted)",
          textDecoration: "underline",
          textDecorationColor: "var(--border-strong)",
          textUnderlineOffset: 3,
        }}
      >
        OpenTelemetry SDK docs ↗
      </a>
    </div>
  );
}
