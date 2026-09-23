import { createModal } from "@buiducnhat/better-modal";
import { Trans, useLingui } from "@lingui/react/macro";
import { FileSpreadsheet, FileText, Presentation } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { useKeyedState } from "@/lib/use-keyed-state";
import type { DocumentKind } from "@/lib/api";

type Props = {
  kind: DocumentKind;
  destination: string;
};

// eslint-disable-next-line lingui/no-unlocalized-strings -- file extension
const EXT_DOCX = ".docx";
// eslint-disable-next-line lingui/no-unlocalized-strings -- file extension
const EXT_XLSX = ".xlsx";
// eslint-disable-next-line lingui/no-unlocalized-strings -- file extension
const EXT_PPTX = ".pptx";

const EXTENSIONS: Record<DocumentKind, string> = {
  document: EXT_DOCX,
  spreadsheet: EXT_XLSX,
  presentation: EXT_PPTX,
};

export const NewDocumentDialog = createModal<Props, string | null>(
  "new-document",
  ({ kind, destination, modal }) => {
    const { t } = useLingui();

    let defaultName = t`Untitled document`;
    if (kind === "spreadsheet") {
      defaultName = t`Untitled spreadsheet`;
    } else if (kind === "presentation") {
      defaultName = t`Untitled presentation`;
    }

    const [name, setName] = useKeyedState(`${kind}:${modal.visible}`, defaultName);
    const trimmed = name.trim();
    const ext = EXTENSIONS[kind] ?? "";

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {kind === "spreadsheet" && <FileSpreadsheet className="size-4 shrink-0" />}
              {kind === "presentation" && <Presentation className="size-4 shrink-0" />}
              {kind === "document" && <FileText className="size-4 shrink-0" />}
              {kind === "spreadsheet" && <Trans>New spreadsheet</Trans>}
              {kind === "presentation" && <Trans>New presentation</Trans>}
              {kind === "document" && <Trans>New document</Trans>}
            </DialogTitle>
            <DialogDescription>
              <Trans>Creates it in {destination} and opens it in the editor.</Trans>
            </DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (!trimmed) return;
              modal.resolve(trimmed);
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="new-document-name">
                <Trans>Name</Trans>
              </Label>
              <div className="flex items-center gap-2">
                <Input
                  id="new-document-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  autoFocus
                  required
                  onFocus={(e) => e.currentTarget.select()}
                />
                <span className="text-muted-foreground text-sm font-normal">{ext}</span>
              </div>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => modal.resolve(null)}>
                <Trans>Cancel</Trans>
              </Button>
              <Button type="submit" disabled={!trimmed}>
                <Trans>Create</Trans>
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    );
  },
);
