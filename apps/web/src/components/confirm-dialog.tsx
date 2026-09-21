import { TriangleAlert } from "lucide-react";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@workspace/ui/components/alert-dialog";
import type { Node } from "@/lib/api";

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * Already-translated text: every caller phrases its own prompt, and most
   * interpolate a name into it, so the sentence is built where it is known
   * rather than assembled from fragments here.
   */
  title: string;
  description: string;
  confirmLabel?: string;
  confirmVariant?: "destructive" | "default";
  onConfirm: () => void;
  /** Optional node for singular/plural phrasing */
  node?: Node | null;
  /**
   * A consequence the plain description cannot carry, set apart in the
   * destructive colour — typically that the action reaches work other people
   * created. Rendered inside the described region so a screen reader reads it
   * with the rest of the prompt; omitted when there is nothing extra to say.
   */
  warning?: string;
}

/**
 * Base confirmation dialog for destructive or unsafe actions.
 * Wraps the shadcn AlertDialog primitive.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  confirmVariant = "destructive",
  onConfirm,
  warning,
}: ConfirmDialogProps) {
  const { t } = useLingui();
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>
            {description}
            {warning && (
              <span className="text-destructive mt-2 flex items-start gap-1.5">
                <TriangleAlert className="mt-px size-3.5 shrink-0" />
                {warning}
              </span>
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>
            <Trans>Cancel</Trans>
          </AlertDialogCancel>
          <AlertDialogAction
            variant={confirmVariant}
            onClick={(e) => {
              e.preventDefault();
              onConfirm();
              onOpenChange(false);
            }}
          >
            {confirmLabel ?? t`Confirm`}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
