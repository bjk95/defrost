import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "@/App";
import { makeGrid } from "@/stories/fixtures";
import { makeApiHandlers } from "@/stories/api-handlers";
import { withMockApi } from "@/stories/mock-fetch";

const meta = {
  title: "Pages/SuppressionsPage",
  parameters: { layout: "fullscreen", skipRouter: true },
} satisfies Meta;
export default meta;

type Story = StoryObj;

export const Empty: Story = {
  name: "Suppressions · empty",
  decorators: [
    withMockApi(makeApiHandlers({ grid: makeGrid(), suppressions: [] }), ["/suppressions"]),
  ],
  render: () => <App />,
};

export const Populated: Story = {
  name: "Suppressions · populated",
  decorators: [
    withMockApi(
      makeApiHandlers({
        grid: makeGrid(),
        suppressions: [
          "github.com/acme/api/handlers.TestRefreshToken",
          "github.com/acme/api/handlers.TestRateLimit",
          "github.com/acme/eval.TestLatency",
        ],
      }),
      ["/suppressions"],
    ),
  ],
  render: () => <App />,
};
