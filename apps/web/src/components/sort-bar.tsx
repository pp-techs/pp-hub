import { ArrowUp, ArrowDown } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import type { SortField, SortOrder, NodeView } from "@/lib/api";

interface SortBarProps {
  dir?: string;
  sort: SortField;
  order: SortOrder;
  view: NodeView;
  navigate: (search: { dir?: string; sort: SortField; order: SortOrder; view: NodeView }) => void;
}

const sortItems = [
  { label: "Name", value: "name" as const },
  { label: "Last modified", value: "modified" as const },
  { label: "Size", value: "size" as const },
];

export function SortBar({ dir, sort, order, view, navigate }: SortBarProps) {
  return (
    <div className="flex items-center gap-1">
      <Select
        value={sort}
        items={sortItems}
        onValueChange={(s) => navigate({ dir, sort: s as SortField, order, view })}
      >
        <SelectTrigger className="h-7 w-36 text-xs" size="sm">
          <SelectValue />
        </SelectTrigger>
        <SelectContent align="end">
          {sortItems.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Button
        variant="ghost"
        size="icon-xs"
        className="shrink-0"
        aria-label={order === "asc" ? "Ascending" : "Descending"}
        onClick={() => navigate({ dir, sort, order: order === "asc" ? "desc" : "asc", view })}
      >
        {order === "asc" ? <ArrowUp className="size-3.5" /> : <ArrowDown className="size-3.5" />}
      </Button>
    </div>
  );
}
