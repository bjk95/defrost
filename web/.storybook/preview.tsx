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
        padding: 24,
      }}
    >
      <Story />
    </div>
  );
};

const withProviders: Decorator = (Story) => (
  <QueryClientProvider client={queryClient}>
    <MemoryRouter>
      <Story />
    </MemoryRouter>
  </QueryClientProvider>
);

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
