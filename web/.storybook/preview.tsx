import type { Decorator, Preview } from "@storybook/react-vite";
import { useEffect } from "react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "../src/index.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: 0 } },
});

const withTheme: Decorator = (Story, context) => {
  const theme = (context.globals.theme as "light" | "dark") ?? "light";
  const fullscreen = context.parameters?.layout === "fullscreen";
  useEffect(() => {
    const root = document.documentElement;
    if (theme === "dark") root.classList.add("dark");
    else root.classList.remove("dark");
  }, [theme]);
  return (
    <div
      style={{
        background: "var(--bg)",
        color: "var(--fg)",
        fontFamily: "var(--font-sans)",
        minHeight: "100vh",
        padding: fullscreen ? 0 : 24,
      }}
    >
      <Story />
    </div>
  );
};

// Stories that bring their own MemoryRouter (e.g. page stories that need to
// drive an initial route) opt out via `parameters: { skipRouter: true }`.
// React Router v7 throws if a Router is nested inside another Router, so we
// must give them a router-less wrapper.
const withProviders: Decorator = (Story, context) => {
  const skipRouter = context.parameters?.skipRouter;
  const inner = (
    <QueryClientProvider client={queryClient}>
      <Story />
    </QueryClientProvider>
  );
  if (skipRouter) return inner;
  return <MemoryRouter>{inner}</MemoryRouter>;
};

const preview: Preview = {
  parameters: {
    layout: "fullscreen",
    backgrounds: { disable: true },
    controls: { expanded: true },
  },
  globalTypes: {
    theme: {
      description: "Global theme",
      defaultValue: "light",
      toolbar: {
        title: "Theme",
        icon: "paintbrush",
        items: [
          { value: "light", title: "Light" },
          { value: "dark", title: "Dark" },
        ],
        dynamicTitle: true,
      },
    },
  },
  decorators: [withTheme, withProviders],
};

export default preview;
