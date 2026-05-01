import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "@/App";
import { makeGreenGrid, makeGrid } from "@/stories/fixtures";
import { makeApiHandlers } from "@/stories/api-handlers";
import { withMockApi } from "@/stories/mock-fetch";

const meta = {
  title: "Pages/RunsPage",
  parameters: { layout: "fullscreen", skipRouter: true },
} satisfies Meta;
export default meta;

type Story = StoryObj;

export const Mixed: Story = {
  name: "Runs · mixed status",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGrid() }), ["/runs"])],
  render: () => <App />,
};

export const AllPassing: Story = {
  name: "Runs · all passing",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGreenGrid() }), ["/runs"])],
  render: () => <App />,
};
