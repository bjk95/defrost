import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "@/App";
import { suppression } from "@/lib/utils";
import { makeGrid } from "@/stories/fixtures";
import { makeApiHandlers } from "@/stories/api-handlers";
import { withMockApi } from "@/stories/mock-fetch";

const meta = {
  title: "Pages/SuppressionsPage",
  parameters: { layout: "fullscreen", skipRouter: true },
} satisfies Meta;
export default meta;

type Story = StoryObj;

function seedSuppressions(ids: string[]) {
  for (const existing of suppression.list()) suppression.remove(existing);
  for (const id of ids) suppression.add(id);
}

export const Empty: Story = {
  name: "Suppressions · empty",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGrid() }), ["/suppressions"])],
  render: () => {
    seedSuppressions([]);
    return <App />;
  },
};

export const Populated: Story = {
  name: "Suppressions · populated",
  decorators: [withMockApi(makeApiHandlers({ grid: makeGrid() }), ["/suppressions"])],
  render: () => {
    seedSuppressions([
      "github.com/acme/api/handlers.TestRefreshToken",
      "github.com/acme/api/handlers.TestRateLimit",
      "github.com/acme/eval.TestLatency",
    ]);
    return <App />;
  },
};
