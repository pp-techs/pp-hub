import { useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useLingui } from "@lingui/react/macro";
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { Textarea } from "@workspace/ui/components/textarea";
import { cn } from "@workspace/ui/lib/utils";
import type { NodePerson } from "@/lib/api";
import { presenceInitials } from "@/lib/collab/presence";
import { mentionQueryAt, type MentionQuery, type PickedMention } from "@/lib/comment-mentions";

/** At most this many suggestions under the text box. */
const MAX_SUGGESTIONS = 6;

/**
 * A comment text box with `@` suggestions from the document's people. Picking
 * one inserts `@Name ` and reports the pick; the caller keeps the picks and
 * sends the ones still in the text as `mentions` (see `mentionedIds`).
 *
 * ⌘/Ctrl+Enter submits, Escape cancels (or first closes the suggestions).
 */
export function CommentInput({
  value,
  onChange,
  onSubmit,
  onCancel,
  onMention,
  members,
  placeholder,
  autoFocus,
  disabled,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
  onCancel?: () => void;
  onMention?: (mention: PickedMention) => void;
  /** Suggestion source; omit for a box without mentions (editing a comment). */
  members?: NodePerson[];
  placeholder?: string;
  autoFocus?: boolean;
  disabled?: boolean;
  className?: string;
}) {
  const { t } = useLingui();
  const ref = useRef<HTMLTextAreaElement>(null);
  const [mention, setMention] = useState<MentionQuery | null>(null);
  const [highlighted, setHighlighted] = useState(0);

  const suggestions = useMemo(() => {
    if (!mention || !members) return [];
    const q = mention.query.toLocaleLowerCase();
    return members
      .filter((m) => [m.name, m.email].some((field) => field?.toLocaleLowerCase().includes(q)))
      .slice(0, MAX_SUGGESTIONS);
  }, [mention, members]);

  const open = suggestions.length > 0;

  const track = (text: string, caret: number) => {
    const next = members ? mentionQueryAt(text, caret) : null;
    setMention(next);
    if (next?.query !== mention?.query) setHighlighted(0);
  };

  const pick = (member: NodePerson) => {
    if (!mention) return;
    const label = member.name || member.email;
    const end = mention.start + 1 + mention.query.length;
    const insert = `@${label} `;
    onChange(value.slice(0, mention.start) + insert + value.slice(end));
    onMention?.({ id: member.user_id, label });
    setMention(null);
    const caret = mention.start + insert.length;
    requestAnimationFrame(() => {
      ref.current?.focus();
      ref.current?.setSelectionRange(caret, caret);
    });
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (open) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        const step = event.key === "ArrowDown" ? 1 : -1;
        setHighlighted((i) => (i + step + suggestions.length) % suggestions.length);
        return;
      }
      if (event.key === "Enter" || event.key === "Tab") {
        event.preventDefault();
        const member = suggestions[highlighted];
        if (member) pick(member);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        setMention(null);
        return;
      }
    }
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      if (value.trim()) onSubmit();
      return;
    }
    if (event.key === "Escape" && onCancel) {
      event.preventDefault();
      event.stopPropagation();
      onCancel();
    }
  };

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      <Textarea
        ref={ref}
        value={value}
        rows={2}
        autoFocus={autoFocus}
        disabled={disabled}
        placeholder={placeholder}
        aria-label={placeholder ?? t`Comment`}
        aria-autocomplete={members ? "list" : undefined}
        aria-expanded={members ? open : undefined}
        className="max-h-40 min-h-14"
        onChange={(event) => {
          onChange(event.target.value);
          track(event.target.value, event.target.selectionStart);
        }}
        onSelect={(event) => track(event.currentTarget.value, event.currentTarget.selectionStart)}
        onBlur={() => setMention(null)}
        onKeyDown={onKeyDown}
      />
      {open ? (
        <ul
          role="listbox"
          aria-label={t`People to mention`}
          className="bg-popover ring-foreground/10 flex flex-col rounded-md p-1 shadow-sm ring-1"
        >
          {suggestions.map((member, i) => {
            const label = member.name || member.email;
            return (
              <li
                key={member.user_id}
                role="option"
                aria-selected={i === highlighted}
                className={cn(
                  "flex cursor-pointer items-center gap-2 rounded-sm px-1.5 py-1 text-xs",
                  i === highlighted && "bg-accent text-accent-foreground",
                )}
                // mousedown, not click: the textarea's blur would close the list first.
                onMouseDown={(event) => {
                  event.preventDefault();
                  pick(member);
                }}
                onMouseEnter={() => setHighlighted(i)}
              >
                <Avatar size="sm" className="size-5">
                  <AvatarImage src={member.avatar_url || undefined} />
                  <AvatarFallback className="text-[9px]">{presenceInitials(label)}</AvatarFallback>
                </Avatar>
                <span className="truncate font-medium">{label}</span>
                {label !== member.email ? (
                  <span className="text-muted-foreground truncate">{member.email}</span>
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : null}
    </div>
  );
}
