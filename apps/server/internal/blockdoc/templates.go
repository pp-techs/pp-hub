package blockdoc

import (
	"embed"
	"errors"
	"strings"
)

// Templates a new block document can start from, one markdown file per
// language under templates/<lang>/<id>.md. They are written in the v1 block
// set and in the form mdoc.Render writes, so a document made from one opens
// editable, is not lossy, and is not rewritten by its first save.
//
//go:embed templates/*/*.md
var templatesFS embed.FS

// ErrUnknownTemplate refuses a template id that is not in the catalog.
var ErrUnknownTemplate = errors.New("unknown template")

// Template is a catalog entry, named in the caller's language.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// templateOrder is the catalog, in the order a picker shows it.
var templateOrder = []string{"meeting-notes", "project-spec", "runbook", "weekly-report", "outline"}

// templateText names and describes each template per language.
var templateText = map[string]map[string][2]string{
	"en": {
		"meeting-notes": {"Meeting notes", "Agenda, discussion, decisions and action items."},
		"project-spec":  {"Project spec", "Problem, goals, proposal, milestones and open questions."},
		"runbook":       {"Runbook", "Step-by-step procedure with checks, rollback and escalation."},
		"weekly-report": {"Weekly report", "Highlights, progress, blockers and next week's plan."},
		"outline":       {"Blank with outline", "An empty document with a heading structure to fill in."},
	},
	"vi": {
		"meeting-notes": {"Biên bản họp", "Chương trình, thảo luận, quyết định và việc cần làm."},
		"project-spec":  {"Đặc tả dự án", "Vấn đề, mục tiêu, đề xuất, các mốc và câu hỏi mở."},
		"runbook":       {"Sổ tay vận hành", "Quy trình từng bước, kiểm tra, khôi phục và leo thang."},
		"weekly-report": {"Báo cáo tuần", "Điểm nổi bật, tiến độ, vướng mắc và kế hoạch tuần tới."},
		"outline":       {"Trang trống có dàn ý", "Tài liệu trống với sẵn cấu trúc tiêu đề để điền."},
	},
}

// TemplateLanguage is the catalog language for a locale tag: Vietnamese for
// any "vi" tag, English for everything else — the rule office.Template uses.
func TemplateLanguage(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "vi") {
		return "vi"
	}
	return "en"
}

// Templates lists the catalog in locale's language.
func Templates(locale string) []Template {
	text := templateText[TemplateLanguage(locale)]
	out := make([]Template, 0, len(templateOrder))
	for _, id := range templateOrder {
		out = append(out, Template{ID: id, Name: text[id][0], Description: text[id][1]})
	}
	return out
}

// TemplateContent is template id's markdown in locale's language, or
// ErrUnknownTemplate.
func TemplateContent(id, locale string) ([]byte, error) {
	if _, ok := templateText["en"][id]; !ok {
		return nil, ErrUnknownTemplate
	}
	return templatesFS.ReadFile("templates/" + TemplateLanguage(locale) + "/" + id + ".md")
}
