import { useEffect, useMemo, useRef, useState } from "react";
import { Logo } from "./Icons";

// Terminal-style boot screen shown while /api/tests is fetching the
// _defrost branch and parsing run history. ~10–15s on first open
// (cold git clone + NDJSON parse), <1s with a warm clone — wrap in a
// delay-on-mount guard so quick loads don't flash.
//
// Subscribes to /api/loading/progress (SSE) for real phase boundaries
// and stream events. Falls back to an optimistic ease-toward-90% timer
// if the connection fails.
//
// Intentionally always-dark (terminal aesthetic). Background is
// var(--gray-1000) regardless of theme.

type Phase = {
  id: string;
  label: string;
  detail: string;
  weight: number;
};

const PHASES: Phase[] = [
  { id: "connect", label: "Connecting to origin", detail: "git ls-remote origin", weight: 0.06 },
  { id: "clone", label: "Cloning _defrost branch", detail: "git clone --depth=1 --single-branch --branch _defrost", weight: 0.55 },
  { id: "spans", label: "Reading run history", detail: "scanning runs/*.otlp.pb.zst", weight: 0.1 },
  { id: "parse", label: "Parsing test spans", detail: "decoding OTLP ResourceSpans", weight: 0.12 },
  { id: "metrics", label: "Parsing eval metrics", detail: "decoding OTLP ResourceMetrics", weight: 0.08 },
  { id: "index", label: "Indexing tests by run", detail: "grouping by encoded test name", weight: 0.06 },
  { id: "ready", label: "Ready", detail: "dashboard online", weight: 0.03 },
];

const CUM = (() => {
  const out: number[] = [];
  let acc = 0;
  for (const p of PHASES) {
    acc += p.weight;
    out.push(acc);
  }
  return out;
})();

type ServerEvent = { phase?: string; detail?: string; stream?: string };

// Subscribes to /api/loading/progress and falls back to an optimistic
// timer (eases toward 0.9 over 15s) when the SSE connection errors. The
// caller passes `done` from React Query so we always know when the real
// /api/tests response has landed — at that point we snap to 100%.
function useProgressDriver(done: boolean) {
  const [events, setEvents] = useState<ServerEvent[]>([]);
  const [fallbackProgress, setFallbackProgress] = useState(0);

  useEffect(() => {
    const es = new EventSource("/api/loading/progress");
    es.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as ServerEvent;
        if (ev.phase || ev.stream) setEvents((prev) => [...prev, ev]);
      } catch {
        /* ignore malformed events */
      }
    };
    es.onerror = () => es.close();
    return () => es.close();
  }, []);

  // Optimistic fallback: only runs while SSE has produced no events yet,
  // so we never animate progress backwards.
  useEffect(() => {
    if (done) {
      setFallbackProgress(1);
      return;
    }
    if (events.length > 0) return;
    let raf = 0;
    const start = performance.now();
    const loop = () => {
      const t = Math.min(1, (performance.now() - start) / 15000);
      setFallbackProgress(0.9 * (1 - Math.pow(1 - t, 1.4)));
      if (t < 1) raf = requestAnimationFrame(loop);
    };
    raf = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(raf);
  }, [done, events.length]);

  const phaseIdx = useMemo(() => {
    for (let i = events.length - 1; i >= 0; i--) {
      const id = events[i].phase;
      if (!id) continue;
      const idx = PHASES.findIndex((p) => p.id === id);
      if (idx >= 0) return idx;
    }
    return 0;
  }, [events]);

  const progress = useMemo(() => {
    if (done) return 1;
    if (events.length === 0) return fallbackProgress;
    // Snap to start of current phase. Within-phase progress is signaled
    // via stream events in the log feed; the bar stays steady.
    const start = phaseIdx === 0 ? 0 : CUM[phaseIdx - 1];
    return Math.min(0.97, start + 0.02);
  }, [done, events.length, phaseIdx, fallbackProgress]);

  return { events, progress, phaseIdx };
}

type Line = {
  k: "head" | "info" | "active" | "done" | "stream";
  t: string;
  text: string;
  sub?: string;
  ts: string;
};

function buildLines(events: ServerEvent[], done: boolean): Line[] {
  const out: Line[] = [];
  out.push({ k: "head", t: "→", text: "defrost serve --port 6969", ts: "00.00" });
  out.push({ k: "info", t: " ", text: "loading data branch from origin", ts: "00.01" });

  // Walk events; each phase event opens a phase line, subsequent stream
  // events belong to that phase until the next phase.
  let lastPhaseLineIdx = -1;
  for (const ev of events) {
    if (ev.phase) {
      const idx = PHASES.findIndex((p) => p.id === ev.phase);
      if (idx < 0) continue;
      const phase = PHASES[idx];
      const t0 = (idx === 0 ? 0 : CUM[idx - 1]) * 15;
      out.push({
        k: "done",
        t: "✓",
        text: phase.label,
        sub: ev.detail || phase.detail,
        ts: t0.toFixed(2).padStart(5, "0"),
      });
      lastPhaseLineIdx = out.length - 1;
    } else if (ev.stream && lastPhaseLineIdx >= 0) {
      out.push({ k: "stream", t: " ", text: ev.stream, ts: "" });
    }
  }

  // Promote the last phase line to "active" unless we're done.
  if (!done && lastPhaseLineIdx >= 0) {
    out[lastPhaseLineIdx].k = "active";
    out[lastPhaseLineIdx].t = "▸";
  }

  return out;
}

export function LoadingScreen({ done = false }: { done?: boolean }) {
  const { events, progress, phaseIdx } = useProgressDriver(done);
  const logRef = useRef<HTMLDivElement>(null);

  const lines = useMemo(() => buildLines(events, done), [events, done]);

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [lines.length]);

  const eta = Math.max(0, Math.ceil((1 - progress) * 15));
  const activePhaseLabel = PHASES[phaseIdx]?.label.toLowerCase() ?? "booting";

  return (
    <div
      style={{
        width: "100%",
        height: "100vh",
        background: "var(--gray-1000)",
        color: "var(--gray-50)",
        fontFamily: "var(--font-mono)",
        display: "flex",
        flexDirection: "column",
        padding: "40px 48px",
        gap: 24,
        position: "relative",
        overflow: "hidden",
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
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            fontSize: 11,
            color: "oklch(1 0 0 / 0.45)",
          }}
        >
          <span
            style={{
              fontSize: 11,
              fontWeight: 500,
              letterSpacing: "0.08em",
              textTransform: "uppercase",
              color: "inherit",
            }}
          >
            Booting
          </span>
          <span style={{ width: 1, height: 10, background: "oklch(1 0 0 / 0.18)" }} />
          <span>~{eta}s</span>
        </div>
      </div>

      <div
        ref={logRef}
        aria-live="polite"
        aria-atomic="false"
        style={{
          flex: 1,
          minHeight: 0,
          overflow: "hidden",
          fontSize: 12.5,
          lineHeight: 1.7,
          color: "oklch(1 0 0 / 0.55)",
          position: "relative",
        }}
      >
        {lines.map((ln, i) => {
          const isActive = ln.k === "active";
          const isDone = ln.k === "done";
          const isStream = ln.k === "stream";
          const isHead = ln.k === "head";
          const color = isActive
            ? "var(--terminal-fg)"
            : isDone
              ? "oklch(1 0 0 / 0.85)"
              : isHead
                ? "oklch(1 0 0 / 0.9)"
                : isStream
                  ? "oklch(1 0 0 / 0.38)"
                  : "oklch(1 0 0 / 0.55)";
          const tColor = isActive
            ? "var(--terminal-fg)"
            : isDone
              ? "var(--success)"
              : "oklch(1 0 0 / 0.35)";
          return (
            <div
              key={i}
              style={{
                display: "grid",
                gridTemplateColumns: "44px 14px 1fr",
                gap: 8,
                color,
                animation:
                  i === lines.length - 1 ? "defrostFadeInUp 280ms var(--ease-out)" : "none",
              }}
            >
              <span
                style={{
                  color: "oklch(1 0 0 / 0.28)",
                  textAlign: "right",
                  fontVariantNumeric: "tabular-nums",
                }}
              >
                {ln.ts || ""}
              </span>
              <span style={{ color: tColor, fontWeight: isActive ? 600 : 400 }}>{ln.t}</span>
              <span>
                {ln.text}
                {isActive && ln.sub && (
                  <span
                    style={{
                      marginLeft: 8,
                      color: "oklch(1 0 0 / 0.4)",
                      fontSize: 11,
                    }}
                  >
                    {ln.sub}
                  </span>
                )}
                {isActive && <BlinkingCursor />}
              </span>
            </div>
          );
        })}
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            fontSize: 11,
            color: "oklch(1 0 0 / 0.5)",
          }}
        >
          <span>{(progress * 100).toFixed(0)}%</span>
          <span>{activePhaseLabel}</span>
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
              width: `${progress * 100}%`,
              background: "var(--terminal-fg)",
              transition: "width 200ms linear",
              boxShadow: "0 0 8px var(--terminal-fg)",
            }}
          />
        </div>
      </div>

      <style>{`
        @keyframes defrostFadeInUp {
          from { opacity: 0; transform: translateY(4px); }
          to   { opacity: 1; transform: translateY(0); }
        }
        @keyframes defrostBlink { 50% { opacity: 0; } }
        @media (prefers-reduced-motion: reduce) {
          * { animation: none !important; transition: none !important; }
        }
      `}</style>
    </div>
  );
}

function BlinkingCursor() {
  return (
    <span
      style={{
        display: "inline-block",
        width: 7,
        height: 13,
        marginLeft: 4,
        marginBottom: -2,
        background: "var(--terminal-fg)",
        animation: "defrostBlink 1s steps(2) infinite",
      }}
    />
  );
}
