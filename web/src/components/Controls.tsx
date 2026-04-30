import { Icon } from "./Icons";

export function SearchInput({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
}) {
  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 8,
        padding: "0 10px",
        height: 32,
        border: "1px solid var(--border)",
        borderRadius: 8,
        background: "var(--bg)",
        minWidth: 240,
      }}
    >
      <span style={{ color: "var(--fg-muted)" }}><Icon.Search /></span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        style={{
          border: "none",
          outline: "none",
          background: "transparent",
          fontFamily: "var(--font-sans)",
          fontSize: 13,
          color: "var(--fg)",
          flex: 1,
          minWidth: 0,
        }}
      />
      {value && (
        <button
          onClick={() => onChange("")}
          style={{
            border: "none",
            background: "transparent",
            color: "var(--fg-muted)",
            cursor: "pointer",
            padding: 0,
            fontSize: 12,
          }}
        >
          ×
        </button>
      )}
    </div>
  );
}

export function Segmented<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (next: T) => void;
  options: Array<{ value: T; label: string }>;
}) {
  return (
    <div
      style={{
        display: "inline-flex",
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: 2,
        background: "var(--bg-subtle)",
        height: 32,
      }}
    >
      {options.map((o) => {
        const active = o.value === value;
        return (
          <button
            key={o.value}
            onClick={() => onChange(o.value)}
            style={{
              padding: "0 10px",
              fontSize: 12,
              fontWeight: 500,
              border: "none",
              borderRadius: 6,
              background: active ? "var(--bg)" : "transparent",
              color: active ? "var(--fg)" : "var(--fg-muted)",
              boxShadow: active ? "var(--shadow-xs)" : "none",
              cursor: "pointer",
              fontFamily: "var(--font-sans)",
            }}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
