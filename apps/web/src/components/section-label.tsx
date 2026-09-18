/** Uppercase 11px heading that opens a section of a page or a form. */
export function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-muted-foreground mb-1 px-0.5 text-[11px] font-medium tracking-wider uppercase">
      {children}
    </div>
  );
}
