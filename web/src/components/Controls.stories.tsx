import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { SearchInput, Segmented } from "./Controls";

const meta = {
  title: "Controls",
} satisfies Meta;
export default meta;

type Story = StoryObj;

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 16, padding: "10px 0" }}>
      <span
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          color: "var(--fg-muted)",
          width: 110,
          display: "inline-block",
        }}
      >
        {label}
      </span>
      {children}
    </div>
  );
}

export const SearchInputStates: Story = {
  name: "SearchInput · empty + filled",
  render: () => {
    const [a, setA] = useState("");
    const [b, setB] = useState("TestLogin");
    return (
      <div style={{ maxWidth: 480 }}>
        <Row label="empty">
          <SearchInput value={a} onChange={setA} placeholder="Filter tests…" />
        </Row>
        <Row label="filled">
          <SearchInput value={b} onChange={setB} placeholder="Filter tests…" />
        </Row>
      </div>
    );
  },
};

export const SegmentedFilters: Story = {
  name: "Segmented · status + window",
  render: () => {
    const [status, setStatus] = useState<"all" | "failing" | "flaky">("all");
    const [window, setWindow] = useState<"10" | "20" | "50">("20");
    return (
      <div style={{ maxWidth: 520 }}>
        <Row label="status">
          <Segmented
            value={status}
            onChange={setStatus}
            options={[
              { value: "all", label: "All" },
              { value: "failing", label: "Failing" },
              { value: "flaky", label: "Flaky" },
            ]}
          />
        </Row>
        <Row label="window">
          <Segmented
            value={window}
            onChange={setWindow}
            options={[
              { value: "10", label: "10 runs" },
              { value: "20", label: "20" },
              { value: "50", label: "50" },
            ]}
          />
        </Row>
        <Row label="binary">
          <Segmented
            value={status === "all" ? "all" : "failing"}
            onChange={(v) => setStatus(v as "all" | "failing")}
            options={[
              { value: "all", label: "All" },
              { value: "failing", label: "Failing only" },
            ]}
          />
        </Row>
      </div>
    );
  },
};
