import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  CloudUpload,
  FolderPlus,
  FolderUp,
  LayoutGrid,
  List,
} from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { ButtonGroup } from "@workspace/ui/components/button-group";
import { cn } from "@workspace/ui/lib/utils";
import type { NodeView, SortField, SortOrder } from "@/lib/api";

const sortLabels: Record<SortField, string> = {
  name: "Name",
  modified: "Modified",
  size: "Size",
};

export interface DriveToolbarProps {
  sort: SortField;
  order: SortOrder;
  view: NodeView;
  /** Viewers get the sort and view controls but no way to add anything. */
  canUpload: boolean;
  onUploadFiles: () => void;
  onUploadFolder: () => void;
  onNewFolder: () => void;
  onSortChange: (sort: SortField) => void;
  onOrderToggle: () => void;
  onViewChange: (view: NodeView) => void;
}

/**
 * Row 2 of the main column: creation actions on the left, sort and view
 * controls on the right, at the same 40px height as the breadcrumb header.
 */
export function DriveToolbar({
  sort,
  order,
  view,
  canUpload,
  onUploadFiles,
  onUploadFolder,
  onNewFolder,
  onSortChange,
  onOrderToggle,
  onViewChange,
}: DriveToolbarProps) {
  return (
    <div className="border-border flex h-10 shrink-0 items-center justify-between gap-2 border-b px-4">
      <div className="flex items-center gap-2">
        {canUpload && (
          <>
            {/* Uploading a folder is rare enough to live behind the caret, but
            common enough that it must not require zipping first. */}
            <ButtonGroup>
              <Button onClick={onUploadFiles}>
                <CloudUpload data-icon="inline-start" /> Upload
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={<Button size="icon" aria-label="More upload options" />}
                >
                  <ChevronDown />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  <DropdownMenuItem onClick={onUploadFiles}>
                    <CloudUpload data-icon="inline-start" /> Upload files
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={onUploadFolder}>
                    <FolderUp data-icon="inline-start" /> Upload folder
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </ButtonGroup>
            <Button variant="outline" onClick={onNewFolder}>
              <FolderPlus data-icon="inline-start" /> New folder
            </Button>
          </>
        )}
      </div>

      <div className="flex items-center gap-2">
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="ghost" />}>
            {sortLabels[sort]}
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuRadioGroup
              value={sort}
              onValueChange={(v) => onSortChange(v as SortField)}
            >
              {(Object.keys(sortLabels) as SortField[]).map((field) => (
                <DropdownMenuRadioItem key={field} value={field}>
                  {sortLabels[field]}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={order === "asc" ? "Sort ascending" : "Sort descending"}
          onClick={onOrderToggle}
        >
          {order === "asc" ? <ArrowUp /> : <ArrowDown />}
        </Button>

        <div className="border-border flex items-center gap-0.5 rounded-md border p-0.5">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Grid view"
            aria-pressed={view === "grid"}
            className={cn(view === "grid" && "bg-accent text-foreground")}
            onClick={() => onViewChange("grid")}
          >
            <LayoutGrid />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="List view"
            aria-pressed={view === "list"}
            className={cn(view === "list" && "bg-accent text-foreground")}
            onClick={() => onViewChange("list")}
          >
            <List />
          </Button>
        </div>
      </div>
    </div>
  );
}
