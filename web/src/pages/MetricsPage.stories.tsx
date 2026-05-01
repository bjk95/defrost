import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "@/App";
import { makeGrid, makeMetrics } from "@/stories/fixtures";
import { makeApiHandlers } from "@/stories/api-handlers";
import { withMockApi } from "@/stories/mock-fetch";
import type { MetricSeries } from "@/lib/metrics";

const meta = {
  title: "Pages/MetricsPage",
  parameters: { layout: "fullscreen", skipRouter: true },
} satisfies Meta;
export default meta;

type Story = StoryObj;

const ALL = makeMetrics();

function only(names: string[]): MetricSeries[] {
  return ALL.filter((m) => names.includes(m.name));
}

export const FullCatalog: Story = {
  name: "Metrics · gauge / sum / histogram",
  decorators: [
    withMockApi(makeApiHandlers({ grid: makeGrid(), metrics: ALL }), ["/metrics"]),
  ],
  render: () => <App />,
};

export const GaugeOnly: Story = {
  name: "Metrics · gauge selected (eval.factuality)",
  decorators: [
    withMockApi(
      makeApiHandlers({ grid: makeGrid(), metrics: only(["eval.factuality"]) }),
      ["/metrics"],
    ),
  ],
  render: () => <App />,
};

export const HistogramOnly: Story = {
  name: "Metrics · histogram heatmap",
  decorators: [
    withMockApi(
      makeApiHandlers({ grid: makeGrid(), metrics: only(["http.server.duration"]) }),
      ["/metrics"],
    ),
  ],
  render: () => <App />,
};
