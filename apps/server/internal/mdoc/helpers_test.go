package mdoc

import (
	"reflect"
	"strings"
	"testing"
)

const helpersMD = `# Plan <!-- slash:mention {"id":"user:a","label":"Ann"} -->@Ann<!-- /slash:mention -->

Intro **bold** and ` + "`code`" + `, see <!-- slash:mention {"id":"doc:d","label":"Spec"} -->@Spec<!-- /slash:mention -->.  
Second line.

![diagram](attachment:aaa)

![remote](https://example.com/x.png)

<!-- slash:video {"poster":"attachment:ppp"} -->
[attachment:vvv](attachment:vvv)

<!-- slash:embed {"mode":"bookmark","description":"About it","thumbnail":"attachment:ttt"} -->
[Example](https://example.com)

- item <!-- slash:mention {"id":"user:b","label":"Bo"} -->@Bo<!-- /slash:mention -->
  - nested again <!-- slash:mention {"id":"user:a","label":"Ann"} -->@Ann<!-- /slash:mention -->

| h1 | h2 |
| --- | --- |
| c1 | <!-- slash:mention {"label":"guest"} -->@guest<!-- /slash:mention --> |

> [!NOTE]
> Callout :tada: text at 12:30:45.

<details>
<summary>Toggle title</summary>

Hidden body.

</details>

` + "```go\nfmt.Println(1)\n```" + `

<!-- slash:file {"size":3} -->
[notes.txt](attachment:fff)

![again](attachment:aaa)
`

func helpersDoc(t *testing.T) Node {
	t.Helper()
	parsed, err := Parse([]byte(helpersMD))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Lossy {
		t.Fatal("helpers document parses lossy")
	}
	return parsed.Doc
}

func TestAttachments(t *testing.T) {
	got := Attachments(helpersDoc(t))
	want := []string{"aaa", "vvv", "ppp", "ttt", "fff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Attachments = %v, want %v", got, want)
	}
	if got := Attachments(Node{Type: "doc"}); got != nil {
		t.Errorf("Attachments(empty) = %v, want nil", got)
	}
}

func TestMentions(t *testing.T) {
	got := Mentions(helpersDoc(t))
	// doc:d is a pageLink after Parse.
	want := []string{"user:a", "user:b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Mentions = %v, want %v", got, want)
	}
	if _, links := PageRefs(helpersDoc(t)); !reflect.DeepEqual(links, []string{"d"}) {
		t.Errorf("page links = %v, want [d]", links)
	}
}

func TestPlainText(t *testing.T) {
	got := PlainText(helpersDoc(t))
	want := strings.Join([]string{
		"Plan @Ann",
		"Intro bold and code, see .\nSecond line.", // a page link has no text
		"diagram",
		"remote",
		"Example",
		"About it",
		"item @Bo",
		"nested again @Ann",
		"h1",
		"h2",
		"c1",
		"@guest",
		"Callout 🎉 text at 12:30:45.",
		"Toggle title",
		"Hidden body.",
		"fmt.Println(1)",
		"notes.txt",
		"again",
	}, "\n")
	if got != want {
		t.Errorf("PlainText:\n got %q\nwant %q", got, want)
	}
}

func TestMapAttachments(t *testing.T) {
	doc := helpersDoc(t)
	before := normalize(t, doc)
	mapped := MapAttachments(doc, func(hash string) string { return "assets/" + hash + ".bin" })
	if normalize(t, doc) != before {
		t.Error("MapAttachments modified its input")
	}
	if got := Attachments(mapped); got != nil {
		t.Errorf("Attachments after mapping every reference = %v, want nil", got)
	}
	md := string(Render(mapped, nil))
	for _, want := range []string{
		"![diagram](assets/aaa.bin)",
		"![remote](https://example.com/x.png)",
		`<!-- slash:video {"poster":"assets/ppp.bin"} -->` + "\n[assets/vvv.bin](assets/vvv.bin)",
		`"thumbnail":"assets/ttt.bin"} -->` + "\n[Example](https://example.com)",
		"[notes.txt](assets/fff.bin)",
		"![again](assets/aaa.bin)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered export lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, AttachmentScheme) {
		t.Errorf("rendered export still references an attachment:\n%s", md)
	}
}
