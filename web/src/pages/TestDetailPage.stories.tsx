import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "@/App";
import { makeGrid } from "@/stories/fixtures";
import { makeApiHandlers } from "@/stories/api-handlers";
import { withMockApi } from "@/stories/mock-fetch";

const meta = {
  title: "Pages/TestDetailPage",
  parameters: { layout: "fullscreen", skipRouter: true },
} satisfies Meta;
export default meta;

type Story = StoryObj;

const STEADY_TEST = "github.com/acme/api/handlers.TestLogin";
const FLAKY_TEST = "github.com/acme/api/handlers.TestRefreshToken";

function url(testId: string): string {
  return `/test?id=${encodeURIComponent(testId)}`;
}

export const SteadyPasses: Story = {
  name: "Test detail · steady passing",
  decorators: [
    withMockApi(makeApiHandlers({ grid: makeGrid(), suppressions: [] }), [url(STEADY_TEST)]),
  ],
  render: () => <App />,
};

export const FlakyHistory: Story = {
  name: "Test detail · flaky",
  decorators: [
    withMockApi(makeApiHandlers({ grid: makeGrid(), suppressions: [] }), [url(FLAKY_TEST)]),
  ],
  render: () => <App />,
};

export const Suppressed: Story = {
  name: "Test detail · suppressed",
  decorators: [
    withMockApi(makeApiHandlers({ grid: makeGrid(), suppressions: [FLAKY_TEST] }), [
      url(FLAKY_TEST),
    ]),
  ],
  render: () => <App />,
};
