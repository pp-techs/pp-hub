package preview

import "testing"

// resolveCSSColors is not a CSS engine — these pin the exact scope it covers
// (literal root custom properties, var()'s own CSS fallback argument) and
// the exact fallback it falls back to when neither applies.
func TestResolveCSSColorsRootLiteral(t *testing.T) {
	svg := `<svg style="--c: #ff0000;"><rect fill="var(--c)"/></svg>`
	got := resolveCSSColors(svg)
	want := `<svg style="--c: #ff0000;"><rect fill="#ff0000"/></svg>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCSSColorsChainedRootVars(t *testing.T) {
	// --b: var(--a) resolves to --a's literal value on an earlier pass than
	// the one that resolves fill's var(--b) — needs the multi-pass loop with
	// props re-extracted each pass, not a single substitution.
	svg := `<svg style="--a: #00ff00; --b: var(--a);"><rect fill="var(--b)"/></svg>`
	got := resolveCSSColors(svg)
	want := `<svg style="--a: #00ff00; --b: #00ff00;"><rect fill="#00ff00"/></svg>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCSSColorsHonorsVarFallbackArg(t *testing.T) {
	// --undefined has no root declaration; var()'s own second argument is
	// exactly what a browser would fall back to.
	svg := `<svg><rect fill="var(--undefined, #0000ff)"/></svg>`
	got := resolveCSSColors(svg)
	want := `<svg><rect fill="#0000ff"/></svg>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCSSColorsFlattensUnresolvable(t *testing.T) {
	cases := map[string]string{
		"undefined, no fallback": `<rect fill="var(--nope)"/>`,
		"bare color-mix":         `<rect fill="color-mix(in srgb, red 50%, blue)"/>`,
		"var of color-mix":       `<rect fill="var(--derived)"/><style>svg{--derived:color-mix(in srgb, red, blue);}</style>`,
	}
	for name, svg := range cases {
		t.Run(name, func(t *testing.T) {
			got := resolveCSSColors(svg)
			if containsColorFunc(got) {
				t.Errorf("resolveCSSColors(%q) = %q, still contains an unresolved color function", svg, got)
			}
		})
	}
}
