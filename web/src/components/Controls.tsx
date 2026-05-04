import { Icon } from "./Icons";
import { cn } from "@/lib/utils";

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
    <div className="inline-flex h-8 min-w-[240px] items-center gap-2 rounded-md border border-border bg-background px-2.5">
      <span className="text-fg-muted">
        <Icon.Search />
      </span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="min-w-0 flex-1 border-none bg-transparent font-sans text-[13px] text-foreground outline-none placeholder:text-fg-muted"
      />
      {value && (
        <button
          type="button"
          onClick={() => onChange("")}
          className="cursor-pointer border-none bg-transparent p-0 text-xs text-fg-muted hover:text-foreground"
          aria-label="Clear search"
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
    <div className="inline-flex h-8 rounded-md border border-border bg-bg-subtle p-0.5">
      {options.map((o) => {
        const active = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            onClick={() => onChange(o.value)}
            className={cn(
              "cursor-pointer rounded-[6px] border-none px-2.5 font-sans text-xs font-medium transition-colors",
              active
                ? "bg-background text-foreground shadow-xs"
                : "bg-transparent text-fg-muted hover:text-foreground",
            )}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
