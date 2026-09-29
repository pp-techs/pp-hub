import { useLinkEditor } from "@slash-editor/react";
import type { Editor } from "@tiptap/core";
import { Trans, useLingui } from "@lingui/react/macro";
import { ExternalLinkIcon, TrashIcon } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@workspace/ui/components/input-group";
import { Popover, PopoverContent } from "@workspace/ui/components/popover";

export function LinkEditorPopover({ editor }: { editor: Editor }) {
  const { t } = useLingui();
  const link = useLinkEditor(editor);

  return (
    <Popover
      open={link.open}
      onOpenChange={(open) => {
        if (!open) link.close();
      }}
    >
      <PopoverContent
        anchor={link.anchor ?? undefined}
        align="start"
        side="bottom"
        sideOffset={8}
        className="w-80 flex-row items-center gap-1 p-1"
      >
        <InputGroup className="flex-1">
          <InputGroupInput
            autoFocus
            type="url"
            value={link.href}
            placeholder={t`Paste a link…`}
            aria-label={t`Link address`}
            onChange={(event) => link.setHref(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                link.confirm();
              }
            }}
          />
          {link.editing ? (
            <InputGroupAddon align="inline-end">
              <InputGroupButton
                size="icon-xs"
                aria-label={t`Open link`}
                disabled={!link.href}
                // eslint-disable-next-line lingui/no-unlocalized-strings -- window target and features
                onClick={() => window.open(link.href, "_blank", "noopener,noreferrer")}
              >
                <ExternalLinkIcon />
              </InputGroupButton>
              <InputGroupButton size="icon-xs" aria-label={t`Remove link`} onClick={link.remove}>
                <TrashIcon />
              </InputGroupButton>
            </InputGroupAddon>
          ) : null}
        </InputGroup>
        <Button
          type="button"
          size="sm"
          variant="secondary"
          disabled={!link.href.trim()}
          onClick={link.confirm}
        >
          {link.editing ? <Trans>Update</Trans> : <Trans>Set link</Trans>}
        </Button>
      </PopoverContent>
    </Popover>
  );
}
