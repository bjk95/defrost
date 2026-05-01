import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "@/App";
import { suppression } from "@/lib/utils";
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

// Wipe and reseed the in-memory suppression list so the "suppressed" story is
// reproducible on reload.
function seedSuppressions(ids: string[]) {
  for (const existing of suppression.list()) suppression.remove(existing);
  for (const id of ids) suppression.add(id);
}

export const SteadyPasses: Story = {
  name: "Test detail · steady passing",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGrid() }), [url(STEADY_TEST)])],
  render: () => {
    seedSuppressions([]);
    return <App />;
  },
};

export const FlakyHistory: Story = {
  name: "Test detail · flaky",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGrid() }), [url(FLAKY_TEST)])],
  render: () => {
    seedSuppressions([]);
    return <App />;
  },
};

export const Suppressed: Story = {
  name: "Test detail · suppressed",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGrid() }), [url(FLAKY_TEST)])],
  render: () => {
    seedSuppressions([FLAKY_TEST]);
    return <App />;
  },
};
