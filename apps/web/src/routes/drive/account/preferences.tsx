import { createFileRoute } from "@tanstack/react-router";
import { Label } from "@workspace/ui/components/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import type { NodeView, SortField, SortOrder } from "@/lib/api";
import { useUiStore } from "@/lib/ui-store";
import { Surface } from "@/components/surface";
import { useTheme } from "@/components/theme-provider";
import { SectionLabel } from "@/components/section-label";

export const Route = createFileRoute("/drive/account/preferences")({
  head: () => ({ meta: [{ title: "Preferences · Account · pp-hub" }] }),
  component: PreferencesPage,
});

const THEMES = [
  { label: "Match the system", value: "system" },
  { label: "Light", value: "light" },
  { label: "Dark", value: "dark" },
];

const VIEWS = [
  { label: "Grid", value: "grid" },
  { label: "List", value: "list" },
];

const SORTS = [
  { label: "Name", value: "name" },
  { label: "Last modified", value: "modified" },
  { label: "Size", value: "size" },
];

const ORDERS = [
  { label: "Ascending", value: "asc" },
  { label: "Descending", value: "desc" },
];

/**
 * How this browser draws the app. Everything here is local to the device —
 * there is no per-user preferences endpoint, and a theme that travelled between
 * machines would be a surprise rather than a feature.
 */
function PreferencesPage() {
  const { theme, setTheme } = useTheme();
  const prefs = useUiStore((s) => s.listingPrefs);
  const setPrefs = useUiStore((s) => s.setListingPrefs);

  return (
    <Surface className="max-w-xl">
      <section className="flex flex-col gap-3">
        <SectionLabel>Appearance</SectionLabel>
        <Field
          id="pref-theme"
          label="Theme"
          hint="Press D anywhere outside a text field to flip between light and dark."
          value={theme}
          items={THEMES}
          onChange={(v) => setTheme(v as "system" | "light" | "dark")}
        />
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>Default listing</SectionLabel>
        <p className="text-muted-foreground text-[10px]">
          Used when a link says nothing about how to draw the folder. A link that carries a view or
          a sort keeps the one it was copied with.
        </p>
        <Field
          id="pref-view"
          label="View"
          value={prefs.view}
          items={VIEWS}
          onChange={(v) => setPrefs({ view: v as NodeView })}
        />
        <Field
          id="pref-sort"
          label="Sort by"
          value={prefs.sort}
          items={SORTS}
          onChange={(v) => setPrefs({ sort: v as SortField })}
        />
        <Field
          id="pref-order"
          label="Direction"
          value={prefs.order}
          items={ORDERS}
          onChange={(v) => setPrefs({ order: v as SortOrder })}
        />
      </section>
    </Surface>
  );
}

function Field({
  id,
  label,
  hint,
  value,
  items,
  onChange,
}: {
  id: string;
  label: string;
  hint?: string;
  value: string;
  items: { label: string; value: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <Label htmlFor={id} className="font-normal">
        {label}
        {hint && <span className="text-muted-foreground block text-[10px]">{hint}</span>}
      </Label>
      <Select value={value} items={items} onValueChange={(v) => onChange((v as string) ?? value)}>
        <SelectTrigger id={id} className="w-44 shrink-0">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
