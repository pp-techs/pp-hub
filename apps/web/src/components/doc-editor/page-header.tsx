import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import type { UploadAdapter } from "@slash-editor/core";
import { searchEmojis } from "@slash-editor/core/emoji";
import { usePage } from "@slash-editor/react";
import type { Editor } from "@tiptap/core";
import { ImageIcon, Loader2, SmilePlusIcon, Trash2Icon } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Popover, PopoverContent, PopoverTrigger } from "@workspace/ui/components/popover";
import { cn } from "@workspace/ui/lib/utils";
import type { AttachmentResult } from "@/lib/collab/attachments";
import type { PpPageStore } from "@/lib/collab/page-store";
import { UNTITLED_PAGE } from "@/components/nodes/page-node-views";

/** How long typing pauses before the title is saved as the node's new name. */
const RENAME_DELAY = 600;
/** Emoji the icon picker shows at once. */
const EMOJI_LIMIT = 80;

function describe(error: unknown): string | undefined {
  return error instanceof Error ? error.message : undefined;
}

/**
 * The open page's cover, icon and title above the document. The title is
 * the node's name: typing renames it once typing pauses, and Enter or ↓
 * moves into the document. The icon (an emoji) and the cover (an attachment
 * of this page) go into the room's `meta` map through the store, so every
 * tab on the page sees them at once and the write-back puts them in the
 * frontmatter. A reader sees the same header without the controls.
 */
export function PageHeader({
  store,
  pageId,
  editor,
  editable,
  adapter,
}: {
  store: PpPageStore;
  pageId: string;
  editor: Editor;
  editable: boolean;
  /** Uploads a cover as an attachment of this page. */
  adapter: UploadAdapter<AttachmentResult>;
}) {
  const { t, i18n } = useLingui();
  const { page } = usePage(store, pageId);
  const [draft, setDraft] = useState<string | null>(null);
  const pending = useRef<{ value: string; timer: number } | null>(null);
  const [uploading, setUploading] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const rename = (value: string) => {
    Promise.resolve(store.update(pageId, { title: value })).catch((error: unknown) =>
      toast.error(t`Could not rename the page`, { description: describe(error) }),
    );
  };

  const flush = () => {
    const queued = pending.current;
    if (!queued) return;
    clearTimeout(queued.timer);
    pending.current = null;
    rename(queued.value);
  };

  // Leaving the page with a rename still waiting saves it, quietly: the
  // header that would report a failure is going away.
  useEffect(
    () => () => {
      const queued = pending.current;
      if (!queued) return;
      clearTimeout(queued.timer);
      pending.current = null;
      Promise.resolve(store.update(pageId, { title: queued.value })).catch(() => undefined);
    },
    [store, pageId],
  );

  const setMeta = (change: { icon?: string; cover?: string }) => {
    Promise.resolve(store.update(pageId, change)).catch((error: unknown) =>
      toast.error(t`Could not change the page`, { description: describe(error) }),
    );
  };

  const uploadCover = async (file: File) => {
    setUploading(true);
    try {
      const result = await adapter.upload(file, { signal: new AbortController().signal });
      setMeta({ cover: result.url });
    } catch (error) {
      toast.error(t`Could not upload the cover`, { description: describe(error) });
    } finally {
      setUploading(false);
    }
  };

  const icon = page?.icon;
  const coverUrl = page && store.coverUrl(page);
  const title = draft ?? page?.title ?? "";
  const reveal =
    // eslint-disable-next-line lingui/no-unlocalized-strings -- class names
    "opacity-0 transition-opacity group-focus-within/page-header:opacity-100 group-hover/page-header:opacity-100";

  const coverButton = editable ? (
    <>
      <input
        ref={fileRef}
        type="file"
        accept="image/*"
        hidden
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) void uploadCover(file);
        }}
      />
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="text-muted-foreground"
        disabled={uploading}
        onClick={() => fileRef.current?.click()}
      >
        {uploading ? <Loader2 className="animate-spin" /> : <ImageIcon />}
        {coverUrl ? <Trans>Change cover</Trans> : <Trans>Add cover</Trans>}
      </Button>
    </>
  ) : null;

  return (
    <header className="group/page-header flex flex-col">
      {coverUrl ? (
        <div className="bg-muted relative mb-2 h-44 w-full overflow-hidden rounded-lg">
          <img src={coverUrl} alt="" className="size-full object-cover" />
          {editable ? (
            <div
              className={cn(
                "bg-background/80 absolute right-2 bottom-2 flex gap-1 rounded-md backdrop-blur",
                reveal,
              )}
            >
              {coverButton}
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="text-muted-foreground"
                onClick={() => setMeta({ cover: "" })}
              >
                <Trash2Icon />
                <Trans>Remove</Trans>
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}

      <div className="flex flex-col gap-1">
        {editable ? (
          <div className={cn("flex items-center gap-1", icon && "flex-col items-start")}>
            <IconPicker
              editor={editor}
              icon={icon}
              onPick={(emoji) => setMeta({ icon: emoji })}
              className={icon ? undefined : reveal}
            />
            {coverUrl ? null : <div className={reveal}>{coverButton}</div>}
          </div>
        ) : icon ? (
          <span aria-hidden className="text-5xl leading-none">
            {icon}
          </span>
        ) : null}

        {editable ? (
          <input
            aria-label={t`Page title`}
            placeholder={i18n._(UNTITLED_PAGE)}
            value={title}
            className="placeholder:text-muted-foreground w-full border-none bg-transparent text-3xl font-bold tracking-tight outline-none"
            onChange={(event) => {
              const value = event.target.value;
              setDraft(value);
              if (pending.current) clearTimeout(pending.current.timer);
              pending.current = { value, timer: window.setTimeout(flush, RENAME_DELAY) };
            }}
            onBlur={() => {
              flush();
              setDraft(null);
            }}
            onKeyDown={(event) => {
              if (event.key !== "Enter" && event.key !== "ArrowDown") return;
              event.preventDefault();
              flush();
              editor.commands.focus("start");
            }}
          />
        ) : (
          <h1 className="text-3xl font-bold tracking-tight">
            {page?.title || i18n._(UNTITLED_PAGE)}
          </h1>
        )}
      </div>
    </header>
  );
}

/** The icon, or "Add icon"; either opens an emoji search drawn from the editor's own dataset. */
function IconPicker({
  editor,
  icon,
  onPick,
  className,
}: {
  editor: Editor;
  icon: string | undefined;
  onPick: (emoji: string) => void;
  className?: string;
}) {
  const { t } = useLingui();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const emojis = open ? searchEmojis(editor.storage.emoji.emojis, query, EMOJI_LIMIT) : [];

  const pick = (emoji: string) => {
    onPick(emoji);
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) setQuery("");
      }}
    >
      {icon ? (
        <PopoverTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              aria-label={t`Change icon`}
              className="size-14 text-5xl"
            />
          }
        >
          {icon}
        </PopoverTrigger>
      ) : (
        <PopoverTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className={cn("text-muted-foreground", className)}
            />
          }
        >
          <SmilePlusIcon />
          <Trans>Add icon</Trans>
        </PopoverTrigger>
      )}
      <PopoverContent align="start" className="w-80">
        <Input
          autoFocus
          value={query}
          placeholder={t`Search emoji`}
          aria-label={t`Search emoji`}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            const first = emojis[0]?.emoji;
            if (event.key === "Enter" && first) {
              event.preventDefault();
              pick(first);
            }
          }}
        />
        <div className="grid max-h-56 grid-cols-8 gap-0.5 overflow-y-auto">
          {emojis.map((item) =>
            item.emoji ? (
              <Button
                key={item.name}
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={item.shortcodes[0] ?? item.name}
                title={`:${item.shortcodes[0] ?? item.name}:`}
                className="text-lg"
                onClick={() => pick(item.emoji!)}
              >
                {item.emoji}
              </Button>
            ) : null,
          )}
        </div>
        {emojis.length === 0 ? (
          <p className="text-muted-foreground text-center text-xs">
            <Trans>No emoji matches.</Trans>
          </p>
        ) : null}
        {icon ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="self-start"
            onClick={() => pick("")}
          >
            <Trash2Icon />
            <Trans>Remove icon</Trans>
          </Button>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}
