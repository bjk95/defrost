import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "@/App";
import { makeGreenGrid, makeGrid } from "@/stories/fixtures";
import { makeApiHandlers } from "@/stories/api-handlers";
import { withMockApi } from "@/stories/mock-fetch";

const meta = {
  title: "Pages/RunDetailPage",
  parameters: { layout: "fullscreen", skipRouter: true },
} satisfies Meta;
export default meta;

type Story = StoryObj;

// Run IDs come from `makeRuns(20)` — newest is run-20, the failing patterns in
// `SPECS` put failures around the early/mid runs (run-04, run-12, run-17).
const RUN_WITH_FAILURES = "run-17";
const PASSING_RUN = "run-20";

function url(runId: string) {
  return `/run?id=${encodeURIComponent(runId)}`;
}

export const PassingRun: Story = {
  name: "Run detail · all passing",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGreenGrid() }), [url(PASSING_RUN)])],
  render: () => <App />,
};

export const RunWithFailures: Story = {
  name: "Run detail · with failures",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGrid() }), [url(RUN_WITH_FAILURES)])],
  render: () => <App />,
};
