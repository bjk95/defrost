import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "@/App";
import { makeGreenGrid, makeGrid } from "@/stories/fixtures";
import { makeApiHandlers } from "@/stories/api-handlers";
import { withMockApi } from "@/stories/mock-fetch";

// Renders the full <App> against a MemoryRouter pointed at /, so the dashboard
// chrome (header + nav + theme toggle) appears exactly as users see it.
const meta = {
  title: "Pages/TestsPage",
  parameters: { layout: "fullscreen", skipRouter: true },
} satisfies Meta;
export default meta;

type Story = StoryObj;

export const MixedHistory: Story = {
  name: "Tests · mixed pass/fail history",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGrid() }), ["/"])],
  render: () => <App />,
};

export const AllGreen: Story = {
  name: "Tests · all green",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGreenGrid() }), ["/"])],
  render: () => <App />,
};
