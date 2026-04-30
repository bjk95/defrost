import { Badge } from "@/components/ui/badge";

export function StatusBadge({ status }: { status: string }) {
  const className =
    status === "pass" ? "bg-green-600 text-white" :
    status === "fail" ? "bg-red-600 text-white" :
    "bg-neutral-300 text-neutral-900";
  return <Badge className={className}>{status}</Badge>;
}
