import { createModal } from "@buiducnhat/better-modal";
import { Trans } from "@lingui/react/macro";
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

type Props = { name: string };

/**
 * The one warning before a lossy markdown file becomes editable. The file
 * holds something the block editor cannot write back (raw HTML, footnotes,
 * …), so the first save rewrites it without that; the untouched original
 * stays one restore away in version history. Resolves `true` to edit anyway.
 */
export const LossyEditModal = createModal<Props, boolean>("lossy-edit", ({ name, modal }) => (
  <AlertDialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(false)}>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>
          <Trans>Edit “{name}” in the block editor?</Trans>
        </AlertDialogTitle>
        <AlertDialogDescription>
          <Trans>
            This file contains formatting the block editor cannot keep, such as raw HTML or
            footnotes. Saving rewrites the file without it. The original stays in version history,
            and you can restore it from there.
          </Trans>
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>
          <Trans>Cancel</Trans>
        </AlertDialogCancel>
        <AlertDialogAction onClick={() => modal.resolve(true)}>
          <Trans>Edit anyway</Trans>
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
));
