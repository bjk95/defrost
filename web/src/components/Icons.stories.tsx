import type { Meta, StoryObj } from "@storybook/react-vite";
import { Icon, Logo } from "./Icons";

const meta = {
  title: "Icons",
} satisfies Meta;
export default meta;

type Story = StoryObj;

function Tile({ name, children }: { name: string; children: React.ReactNode }) {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 10,
        padding: 16,
        border: "1px solid var(--border)",
        borderRadius: 8,
        background: "var(--bg)",
        minHeight: 80,
      }}
    >
      <span style={{ color: "var(--fg)", display: "inline-flex" }}>{children}</span>
      <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
        {name}
      </span>
    </div>
  );
}

export const Catalog: Story = {
  name: "Icon · all glyphs",
  render: () => {
    const entries = Object.entries(Icon) as Array<
      [string, (p: React.SVGProps<SVGSVGElement>) => React.ReactElement]
    >;
    return (
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fill, minmax(120px, 1fr))",
          gap: 8,
          maxWidth: 800,
        }}
      >
        {entries.map(([name, Cmp]) => (
          <Tile key={name} name={name}>
            <Cmp width={20} height={20} />
          </Tile>
        ))}
      </div>
    );
  },
};

export const Sizes: Story = {
  name: "Icon · sizing + colour",
  render: () => (
    <div style={{ display: "flex", flexDirection: "column", gap: 24, maxWidth: 600 }}>
      <div style={{ display: "flex", gap: 16, alignItems: "center" }}>
        {[12, 14, 16, 20, 24, 32].map((s) => (
          <span
            key={s}
            style={{
              display: "inline-flex",
              flexDirection: "column",
              alignItems: "center",
              gap: 4,
              color: "var(--fg)",
            }}
          >
            <Icon.GitBranch width={s} height={s} />
            <span
              style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)" }}
            >
              {s}
            </span>
          </span>
        ))}
      </div>
      <div style={{ display: "flex", gap: 16, alignItems: "center" }}>
        <span style={{ color: "var(--fg)" }}><Icon.Check width={20} height={20} /></span>
        <span style={{ color: "var(--success)" }}><Icon.Check width={20} height={20} /></span>
        <span style={{ color: "var(--danger)" }}><Icon.AlertTriangle width={20} height={20} /></span>
        <span style={{ color: "var(--accent)" }}><Icon.ArrowUpRight width={20} height={20} /></span>
        <span style={{ color: "var(--fg-muted)" }}><Icon.EyeOff width={20} height={20} /></span>
      </div>
    </div>
  ),
};

export const LogoSizes: Story = {
  name: "Logo · sizes",
  render: () => (
    <div style={{ display: "flex", gap: 24, alignItems: "flex-end", color: "var(--fg)" }}>
      {[16, 20, 28, 40, 64].map((s) => (
        <div
          key={s}
          style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 6 }}
        >
          <Logo size={s} />
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
            {s}
          </span>
        </div>
      ))}
    </div>
  ),
};
