import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { ChevronDown, Search, Tag, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Input } from "@workspace/ui/components/input";
import { cn } from "@workspace/ui/lib/utils";
import type { Person } from "@pp-hub/app-sdk";
import {
  type CardFilter,
  type DueFilter,
  isFiltering,
  type Label,
  NO_FILTER,
  UNASSIGNED,
} from "./board-state";
import { LabelChip } from "./label-ui";

// Radio items carry strings; "any" stands for no restriction.
// eslint-disable-next-line lingui/no-unlocalized-strings -- radio value, not copy
const ANY = "any";

type FilterButtonProps = React.ComponentProps<typeof Button> & {
  label: React.ReactNode;
  /** What is selected, when something is; the button then reads as active. */
  selected?: React.ReactNode;
};

/** A menu trigger: it takes the props the menu hands its trigger. */
function FilterButton({ label, selected, className, ...props }: FilterButtonProps) {
  return (
    <Button
      {...props}
      variant="outline"
      className={cn(
        selected !== undefined && "border-primary/50 bg-primary/5 text-primary",
        className,
      )}
    >
      {label}
      {selected !== undefined && <span className="font-semibold">{selected}</span>}
      <ChevronDown className="text-muted-foreground" />
    </Button>
  );
}

interface BoardToolbarProps {
  filter: CardFilter;
  onFilter: (next: CardFilter) => void;
  labels: Label[];
  /** Everyone assigned to a card on the board. */
  people: Person[];
  /** Cards on the board, and how many of them the filter lets through. */
  total: number;
  shown: number;
  columns: number;
  onManageLabels?: () => void;
}

/**
 * The strip under the editor header: narrow the board by text, assignee, label
 * or due date, and see how much of it that leaves. Filtering only changes what
 * is drawn; nothing is saved and other people see nothing of it.
 */
export function BoardToolbar({
  filter,
  onFilter,
  labels,
  people,
  total,
  shown,
  columns,
  onManageLabels,
}: BoardToolbarProps) {
  const { t } = useLingui();
  const filtering = isFiltering(filter);
  const assignee =
    filter.assignee === null
      ? undefined
      : filter.assignee === UNASSIGNED
        ? t`Unassigned`
        : (people.find((p) => p.id === filter.assignee)?.name ?? "");
  const selectedLabels = labels.filter((l) => filter.labels.includes(l.id));
  const dueNames: Record<DueFilter, string> = {
    overdue: t`Overdue`,
    week: t`Next 7 days`,
    none: t`No date`,
  };

  return (
    <div className="bg-muted/30 flex min-h-9 shrink-0 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b px-3 py-1">
      <div className="flex flex-wrap items-center gap-1.5">
        <div className="relative">
          <Search
            aria-hidden
            className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2"
          />
          <Input
            type="search"
            value={filter.query}
            maxLength={200}
            aria-label={t`Filter cards`}
            placeholder={t`Filter cards…`}
            className="w-48 pl-7"
            onChange={(e) => onFilter({ ...filter, query: e.target.value })}
          />
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <FilterButton
                label={<Trans>Assignee</Trans>}
                selected={assignee !== undefined ? <>{assignee}</> : undefined}
              />
            }
          />
          <DropdownMenuContent align="start" className="max-h-72 min-w-44">
            <DropdownMenuRadioGroup
              value={filter.assignee ?? ANY}
              onValueChange={(v) => onFilter({ ...filter, assignee: v === ANY ? null : v })}
            >
              <DropdownMenuRadioItem value={ANY}>
                <Trans>Anyone</Trans>
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value={UNASSIGNED}>
                <Trans>Unassigned</Trans>
              </DropdownMenuRadioItem>
              {people.length > 0 && <DropdownMenuSeparator />}
              {people.map((p) => (
                <DropdownMenuRadioItem key={p.id} value={p.id}>
                  {p.name || p.email}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <FilterButton
                label={<Trans>Label</Trans>}
                selected={
                  selectedLabels.length === 0 ? undefined : selectedLabels.length === 1 ? (
                    <>{selectedLabels[0]!.name}</>
                  ) : (
                    <>{selectedLabels.length}</>
                  )
                }
              />
            }
          />
          <DropdownMenuContent align="start" className="max-h-72 min-w-44">
            {labels.length === 0 ? (
              <p className="text-muted-foreground px-2 py-1.5 text-xs">
                <Trans>No labels on this board yet.</Trans>
              </p>
            ) : (
              labels.map((label) => (
                <DropdownMenuCheckboxItem
                  key={label.id}
                  checked={filter.labels.includes(label.id)}
                  onCheckedChange={(on) =>
                    onFilter({
                      ...filter,
                      labels: on
                        ? [...filter.labels, label.id]
                        : filter.labels.filter((id) => id !== label.id),
                    })
                  }
                >
                  <LabelChip label={label} />
                </DropdownMenuCheckboxItem>
              ))
            )}
          </DropdownMenuContent>
        </DropdownMenu>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <FilterButton
                label={<Trans>Due</Trans>}
                selected={filter.due === null ? undefined : <>{dueNames[filter.due]}</>}
              />
            }
          />
          <DropdownMenuContent align="start" className="min-w-40">
            <DropdownMenuRadioGroup
              value={filter.due ?? ANY}
              onValueChange={(v) =>
                onFilter({ ...filter, due: v === ANY ? null : (v as DueFilter) })
              }
            >
              <DropdownMenuRadioItem value={ANY}>
                <Trans>Any date</Trans>
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="overdue">{dueNames.overdue}</DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="week">{dueNames.week}</DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="none">{dueNames.none}</DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        {filtering && (
          <Button
            variant="ghost"
            className="text-muted-foreground"
            onClick={() => onFilter(NO_FILTER)}
          >
            <X />
            <Trans>Clear</Trans>
          </Button>
        )}
      </div>

      <div className="flex items-center gap-2">
        <span className="text-muted-foreground text-[0.625rem] tabular-nums" aria-live="polite">
          {filtering && (
            <Trans>
              {shown} of <Plural value={total} one="# card" other="# cards" />
            </Trans>
          )}
          {!filtering && <Plural value={total} one="# card" other="# cards" />}
          {" · "}
          <Plural value={columns} one="# column" other="# columns" />
        </span>
        {onManageLabels && (
          <Button variant="ghost" className="text-muted-foreground" onClick={onManageLabels}>
            <Tag />
            <Trans>Manage labels</Trans>
          </Button>
        )}
      </div>
    </div>
  );
}
