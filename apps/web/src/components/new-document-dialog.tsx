import { createModal } from "@buiducnhat/better-modal";
import { Trans, useLingui } from "@lingui/react/macro";
import { FileSpreadsheet, FileText, NotebookPen, Presentation } from "lucide-react";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { useKeyedState } from "@/lib/use-keyed-state";
import { useMarkdownTemplates } from "@/lib/queries";
import type { DocumentKind } from "@/lib/api";
import { useLocale } from "@/components/locale-provider";

type Props = {
  kind: DocumentKind;
  destination: string;
};

/** What to create: the name, and for a markdown document the template it starts from. */
export type NewDocument = {
  name: string;
  template?: { id: string; locale: string };
};

// eslint-disable-next-line lingui/no-unlocalized-strings -- select value, never shown
const BLANK = "__blank__";

// eslint-disable-next-line lingui/no-unlocalized-strings -- file extension
const EXT_DOCX = ".docx";
// eslint-disable-next-line lingui/no-unlocalized-strings -- file extension
const EXT_XLSX = ".xlsx";
// eslint-disable-next-line lingui/no-unlocalized-strings -- file extension
const EXT_PPTX = ".pptx";
// eslint-disable-next-line lingui/no-unlocalized-strings -- file extension
const EXT_MD = ".md";

const EXTENSIONS: Record<DocumentKind, string> = {
  document: EXT_DOCX,
  spreadsheet: EXT_XLSX,
  presentation: EXT_PPTX,
  markdown: EXT_MD,
};

export const NewDocumentDialog = createModal<Props, NewDocument | null>(
  "new-document",
  ({ kind, destination, modal }) => {
    const { t } = useLingui();
    const locale = useLocale();

    let defaultName = t`Untitled document`;
    if (kind === "spreadsheet") {
      defaultName = t`Untitled spreadsheet`;
    } else if (kind === "presentation") {
      defaultName = t`Untitled presentation`;
    } else if (kind === "markdown") {
      defaultName = t`Untitled`;
    }

    const [name, setName] = useKeyedState(`${kind}:${modal.visible}`, defaultName);
    const [template, setTemplate] = useKeyedState(`${kind}:${modal.visible}`, BLANK);
    const templates = useMarkdownTemplates(locale, modal.visible && kind === "markdown");
    const trimmed = name.trim();
    const ext = EXTENSIONS[kind] ?? "";
    const templateItems = [
      { value: BLANK, label: t`Blank`, description: t`Start from an empty page.` },
      ...(templates.data ?? []).map((tpl) => ({
        value: tpl.id,
        label: tpl.name,
        description: tpl.description,
      })),
    ];
    const chosen = templateItems.find((item) => item.value === template);

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {kind === "spreadsheet" && <FileSpreadsheet className="size-4 shrink-0" />}
              {kind === "presentation" && <Presentation className="size-4 shrink-0" />}
              {kind === "document" && <FileText className="size-4 shrink-0" />}
              {kind === "markdown" && <NotebookPen className="size-4 shrink-0" />}
              {kind === "spreadsheet" && <Trans>New spreadsheet</Trans>}
              {kind === "presentation" && <Trans>New presentation</Trans>}
              {kind === "document" && <Trans>New document</Trans>}
              {kind === "markdown" && <Trans>New markdown document</Trans>}
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
              modal.resolve({
                name: trimmed,
                template:
                  kind === "markdown" && template !== BLANK ? { id: template, locale } : undefined,
              });
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
            {kind === "markdown" ? (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="new-document-template">
                  <Trans>Template</Trans>
                </Label>
                {templates.isPending ? (
                  <Skeleton className="h-8 w-full" />
                ) : (
                  <Select
                    value={template}
                    items={templateItems}
                    onValueChange={(v) => setTemplate((v as string | null) ?? BLANK)}
                  >
                    <SelectTrigger id="new-document-template" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {templateItems.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
                <p className="text-muted-foreground text-xs">
                  {templates.isError
                    ? t`Templates could not be loaded; the document starts blank.`
                    : chosen?.description}
                </p>
              </div>
            ) : null}
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
