import { useEffect, useRef, useState } from "react";
import type { Cell, RunSummary } from "@/types";
import { fmt } from "@/lib/utils";
import { StatusPill } from "./Primitives";

export interface ChartPoint {
  run: RunSummary;
  cell: Cell;
}

export function DurationChart({
  points,
  p50,
  p95,
  selIdx,
  onSelect,
  onOpen,
}: {
  points: ChartPoint[];
  p50: number;
  p95: number;
  selIdx: number;
  onSelect: (idx: number) => void;
  onOpen: (p: ChartPoint) => void;
}) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [w, setW] = useState(900);
  const [hov, setHov] = useState<number | null>(null);

  useEffect(() => {
    if (!ref.current) return;
    const ro = new ResizeObserver((es) => {
      for (const e of es) setW(Math.max(320, Math.floor(e.contentRect.width)));
    });
    ro.observe(ref.current);
    return () => ro.disconnect();
  }, []);

  const H = 220;
  const padT = 16, padB = 26, padL = 44, padR = 12;
  const innerW = w - padL - padR;
  const innerH = H - padT - padB;
  const yMax = Math.max(1, p95 * 1.4, ...points.map((p) => p.cell.duration_ms || 0));
  const yScale = (v: number) => padT + innerH - (v / yMax) * innerH;
  const N = Math.max(1, points.length);
  const slot = innerW / N;
  const barW = Math.max(3, Math.min(18, slot * 0.7));
  const ticks = [0, yMax * 0.25, yMax * 0.5, yMax * 0.75, yMax];

  return (
    <div ref={ref} style={{ position: "relative", width: "100%" }}>
      <svg width={w} height={H} style={{ display: "block" }}>
        {ticks.map((t, i) => (
          <g key={i}>
            <line
              x1={padL} x2={w - padR}
              y1={yScale(t)} y2={yScale(t)}
              stroke="var(--border)" strokeWidth="1"
            />
            <text
              x={padL - 8} y={yScale(t) + 3} textAnchor="end"
              fontSize="10" fontFamily="var(--font-mono)" fill="var(--fg-muted)"
            >
              {t === 0 ? "0" : fmt.duration(Math.round(t))}
            </text>
          </g>
        ))}

        <line
          x1={padL} x2={w - padR}
          y1={yScale(p50)} y2={yScale(p50)}
          stroke="var(--accent)" strokeWidth="1" opacity="0.7"
        />
        <text
          x={w - padR - 4} y={yScale(p50) - 4} textAnchor="end"
          fontSize="10" fontFamily="var(--font-mono)" fill="var(--accent)"
        >
          p50
        </text>
        <line
          x1={padL} x2={w - padR}
          y1={yScale(p95)} y2={yScale(p95)}
          stroke="var(--accent)" strokeDasharray="3 3" strokeWidth="1" opacity="0.7"
        />
        <text
          x={w - padR - 4} y={yScale(p95) - 4} textAnchor="end"
          fontSize="10" fontFamily="var(--font-mono)" fill="var(--accent)"
        >
          p95
        </text>

        {points.map((p, i) => {
          const xCenter = padL + slot * (i + 0.5);
          const x = xCenter - barW / 2;
          const isSkip = p.cell.status === "skip";
          const isFail = p.cell.status === "fail";
          const y = isSkip ? padT + innerH - 2 : yScale(p.cell.duration_ms);
          const h = isSkip ? 2 : Math.max(2, padT + innerH - y);
          const fill = isFail ? "var(--danger)" : isSkip ? "var(--gray-300)" : "var(--success)";
          const isSel = i === selIdx, isHov = i === hov;
          return (
            <g key={p.run.run_id}>
              <rect
                x={padL + slot * i} y={padT}
                width={slot} height={innerH} fill="transparent"
                onMouseEnter={() => setHov(i)}
                onMouseLeave={() => setHov(null)}
                onClick={() => onSelect(i)}
                onDoubleClick={() => onOpen(p)}
                style={{ cursor: "pointer" }}
              />
              <rect
                x={x} y={y} width={barW} height={h} fill={fill} rx="1.5"
                opacity={isSkip ? 0.6 : hov !== null && !isHov && !isSel ? 0.6 : 1}
                style={{ transition: "opacity var(--dur-fast)", pointerEvents: "none" }}
              />
              {isSel && !isSkip && (
                <rect
                  x={x - 1.5} y={y - 1.5}
                  width={barW + 3} height={h + 3}
                  fill="none" stroke="var(--accent)" strokeWidth="1.5" rx="2.5"
                  style={{ pointerEvents: "none" }}
                />
              )}
            </g>
          );
        })}

        <line
          x1={padL} x2={w - padR}
          y1={padT + innerH} y2={padT + innerH}
          stroke="var(--border-strong)" strokeWidth="1"
        />
        {points.length > 0 && (
          <>
            <text
              x={padL} y={H - 8} fontSize="10"
              fontFamily="var(--font-mono)" fill="var(--fg-muted)"
            >
              {fmt.relTime(points[0].run.ts)}
            </text>
            <text
              x={w - padR} y={H - 8}
              fontSize="10" fontFamily="var(--font-mono)" fill="var(--fg-muted)"
              textAnchor="end"
            >
              {fmt.relTime(points[points.length - 1].run.ts)}
            </text>
          </>
        )}
      </svg>

      {hov !== null && points[hov] && (() => {
        const p = points[hov];
        const x = padL + slot * (hov + 0.5);
        const left = Math.max(8, Math.min(w - 220, x - 100));
        return (
          <div
            style={{
              position: "absolute",
              left,
              top: 6,
              pointerEvents: "none",
              background: "var(--bg)",
              border: "1px solid var(--border)",
              borderRadius: 8,
              padding: "8px 10px",
              boxShadow: "var(--shadow-md)",
              minWidth: 200,
              fontSize: 12,
            }}
          >
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: 6,
              }}
            >
              <StatusPill status={p.cell.status} size="xs" />
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 14, fontWeight: 500 }}>
                {fmt.duration(p.cell.duration_ms)}
              </span>
            </div>
            <div
              style={{
                fontFamily: "var(--font-mono)",
                color: "var(--fg-muted)",
                fontSize: 11,
              }}
            >
              {p.run.commit?.slice(0, 7) ?? ""} · {p.run.branch ?? ""}
            </div>
            <div style={{ color: "var(--fg-muted)", fontSize: 11, marginTop: 2 }}>
              {fmt.relTime(p.run.ts)} — click to select, dblclick to open
            </div>
          </div>
        );
      })()}
    </div>
  );
}
