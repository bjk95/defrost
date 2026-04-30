import { useQuery } from "@tanstack/react-query";
import { getTests } from "@/api";
import { TestRow } from "./TestRow";

export function Grid() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["tests"],
    queryFn: getTests,
  });

  if (isLoading) return <p className="text-sm text-muted-foreground">loading…</p>;
  if (isError) return <p className="text-sm text-red-600">failed: {(error as Error).message}</p>;
  if (!data) return null;
  if (data.runs.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No test runs yet — run <code>defrost exec go test ./...</code> to record some.
      </p>
    );
  }

  return (
    <div className="space-y-0">
      <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
        <span className="flex-1">test</span>
        <span>← older · newer →</span>
      </div>
      {data.tests.map((row) => (
        <TestRow key={row.test_id} row={row} runs={[...data.runs].reverse()} />
      ))}
    </div>
  );
}
