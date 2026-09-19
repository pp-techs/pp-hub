import { useRef, useState } from "react";
import type { DragEvent, ReactNode } from "react";
import { UploadCloud } from "lucide-react";
import { collectDropItems } from "@/lib/drop-items";
import { useUploadWithConflicts } from "@/components/upload-conflict-dialog";

interface DropZoneProps {
  spaceId: string;
  /** Folder the drop lands in; undefined is the top level of the space. */
  parentId?: string;
  /** Breadcrumb shown in the overlay so the destination is never a guess. */
  destination: string;
  /** Viewers cannot upload, so the overlay never appears for them. */
  disabled?: boolean;
  children: ReactNode;
}

/**
 * Wraps the file listing and turns a drag over it into an upload.
 *
 * dragenter/dragleave fire for every child element the pointer crosses, so a
 * naive boolean flickers the overlay as the cursor moves across rows. The
 * counter tracks depth instead and only clears at zero.
 */
export function DropZone({ spaceId, parentId, destination, disabled, children }: DropZoneProps) {
  const [over, setOver] = useState(false);
  const depth = useRef(0);
  const upload = useUploadWithConflicts();

  if (disabled) return <>{children}</>;

  const reset = () => {
    depth.current = 0;
    setOver(false);
  };

  const carriesFiles = (e: DragEvent) => [...e.dataTransfer.types].includes("Files");

  return (
    <div
      className="relative flex min-h-0 flex-1 flex-col"
      onDragEnter={(e) => {
        if (!carriesFiles(e)) return;
        e.preventDefault();
        depth.current += 1;
        setOver(true);
      }}
      onDragOver={(e) => {
        if (!carriesFiles(e)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
      }}
      onDragLeave={() => {
        depth.current = Math.max(0, depth.current - 1);
        if (depth.current === 0) setOver(false);
      }}
      onDrop={(e) => {
        if (!carriesFiles(e)) return;
        e.preventDefault();
        reset();
        // Dropping onto a folder that already holds a file of the same name
        // asks before it uploads, exactly as the New menu's picker does.
        void collectDropItems(e.dataTransfer).then((items) => upload(items, spaceId, parentId));
      }}
    >
      {children}

      {over && (
        <div className="bg-primary/5 absolute inset-0 z-40 flex items-center justify-center p-2">
          <div className="border-primary flex h-full w-full flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed">
            <UploadCloud className="text-primary size-10" />
            <p className="text-sm font-medium">Drop files to upload</p>
            <p className="text-muted-foreground text-xs">They will be added to {destination}</p>
            <p className="text-muted-foreground text-[10px]">
              Folders are uploaded with their structure preserved
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
