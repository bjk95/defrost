// Lucide-style stroke icons. Inlined SVG so the bundle stays tiny and
// styling via currentColor is consistent with the design tokens.

import type { SVGProps } from "react";

const base: SVGProps<SVGSVGElement> = {
  viewBox: "0 0 24 24",
  width: 14,
  height: 14,
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.6,
  strokeLinecap: "round",
  strokeLinejoin: "round",
};

export const Icon = {
  Search: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
  ),
  ChevronRight: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} strokeWidth={1.8} {...p}><path d="m9 6 6 6-6 6" /></svg>
  ),
  ChevronDown: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} strokeWidth={1.8} {...p}><path d="m6 9 6 6 6-6" /></svg>
  ),
  ArrowLeft: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}><path d="M19 12H5" /><path d="m12 19-7-7 7-7" /></svg>
  ),
  ArrowUpRight: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}><path d="M7 17 17 7" /><path d="M7 7h10v10" /></svg>
  ),
  GitBranch: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <line x1="6" y1="3" x2="6" y2="15" />
      <circle cx="18" cy="6" r="2.5" />
      <circle cx="6" cy="18" r="2.5" />
      <path d="M18 8.5a6 6 0 0 1-6 6h-3" />
    </svg>
  ),
  GitCommit: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <circle cx="12" cy="12" r="3.5" />
      <path d="M3 12h5.5" />
      <path d="M15.5 12H21" />
    </svg>
  ),
  GitPullRequest: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="6" cy="18" r="2.5" />
      <circle cx="18" cy="18" r="2.5" />
      <path d="M6 8.5v7" />
      <path d="M13 6h3a2 2 0 0 1 2 2v7.5" />
    </svg>
  ),
  Clock: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" />
    </svg>
  ),
  Terminal: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}><path d="m5 8 4 4-4 4" /><path d="M12 18h7" /></svg>
  ),
  User: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <circle cx="12" cy="9" r="3.5" />
      <path d="M5 20a7 7 0 0 1 14 0" />
    </svg>
  ),
  Cpu: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <rect x="6" y="6" width="12" height="12" rx="1.5" />
      <rect x="9.5" y="9.5" width="5" height="5" rx="0.5" />
      <path d="M10 3v2M14 3v2M10 19v2M14 19v2M3 10h2M3 14h2M19 10h2M19 14h2" />
    </svg>
  ),
  Sun: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 3v2M12 19v2M3 12h2M19 12h2M5.5 5.5l1.4 1.4M17.1 17.1l1.4 1.4M5.5 18.5l1.4-1.4M17.1 6.9l1.4-1.4" />
    </svg>
  ),
  Moon: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <path d="M20 14.5A8 8 0 0 1 9.5 4a7.5 7.5 0 1 0 10.5 10.5z" />
    </svg>
  ),
  EyeOff: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} {...p}>
      <path d="M3 3l18 18" />
      <path d="M10.6 6.1A10 10 0 0 1 12 6c5 0 9 4 10 6a13 13 0 0 1-2.6 3.5" />
      <path d="M6.6 6.6C4.2 8.1 2.6 10.4 2 12c1 2 5 6 10 6a10 10 0 0 0 5.4-1.6" />
      <path d="M9.9 9.9a3 3 0 0 0 4.2 4.2" />
    </svg>
  ),
  Check: (p: SVGProps<SVGSVGElement>) => (
    <svg {...base} strokeWidth={1.8} {...p}><path d="M5 12.5 10 17.5 19 7.5" /></svg>
  ),
};

export function Logo({ size = 16 }: { size?: number }) {
  return (
    <svg
      width={size} height={size} viewBox="0 0 100 100"
      fill="none" aria-hidden="true"
    >
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
  );
}
