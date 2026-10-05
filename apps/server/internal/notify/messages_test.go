package notify

import (
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

func TestMessagesRender(t *testing.T) {
	if got := localeOf("vi-VN"); got != "vi" {
		t.Errorf("localeOf(vi-VN) = %q, want vi", got)
	}
	if got := localeOf(""); got != "en" {
		t.Errorf("localeOf('') = %q, want en", got)
	}
	if got := localeOf("en-US"); got != "en" {
		t.Errorf("localeOf('en-US') = %q, want en", got)
	}

	// render("vi", group data) subject contains với nhóm
	viGroup := msgData{
		GranterName:  "Nguyễn Văn A",
		GranterEmail: "a@example.com",
		NodeName:     "Kế hoạch Q3",
		IsDir:        false,
		Group:        "Ban Giám đốc",
		SpaceName:    "Công ty",
		Instance:     "pp-hub",
		Level:        store.ShareLevelView,
		CanDownload:  true,
		Link:         "http://localhost:8080/drive/space-id/shared",
	}
	subjectVI, bodyVI := render("vi", viGroup)
	if !strings.Contains(subjectVI, "với nhóm") {
		t.Errorf("expected subject to contain 'với nhóm', got %q", subjectVI)
	}
	if !strings.Contains(bodyVI, "Quyền: Có thể xem và tải xuống") {
		t.Errorf("expected body to contain 'Quyền: Có thể xem và tải xuống', got %q", bodyVI)
	}
	if !strings.Contains(bodyVI, "Gửi email khi có nội dung được chia sẻ với tôi") {
		t.Errorf("expected body to contain setting opt-out string, got %q", bodyVI)
	}

	// render("en", direct edit) body contains Access: Can edit and the link
	enDirect := msgData{
		GranterName:  "Alice Smith",
		GranterEmail: "alice@example.com",
		NodeName:     "Projects",
		IsDir:        true,
		Group:        "",
		SpaceName:    "Engineering",
		Instance:     "pp-hub",
		Level:        store.ShareLevelEdit,
		CanDownload:  true,
		Link:         "http://localhost:8080/drive/space-id?dir=node-id",
	}
	subjectEN, bodyEN := render("en", enDirect)
	if !strings.Contains(subjectEN, `Alice Smith shared "Projects" with you`) {
		t.Errorf("unexpected subject: %q", subjectEN)
	}
	if !strings.Contains(bodyEN, "Access: Can edit") {
		t.Errorf("expected body to contain 'Access: Can edit', got %q", bodyEN)
	}
	if !strings.Contains(bodyEN, "http://localhost:8080/drive/space-id?dir=node-id") {
		t.Errorf("expected body to contain link, got %q", bodyEN)
	}
	if !strings.Contains(bodyEN, "Email me when something is shared with me") {
		t.Errorf("expected body to contain setting opt-out string, got %q", bodyEN)
	}
}

// An invitation goes to an address that never asked for it, so nothing the
// sender chose may reach the subject, and nothing of theirs in the body may
// be a link.
func TestInviteCarriesNoSenderLinks(t *testing.T) {
	for _, locale := range []string{"en", "vi"} {
		subject, body := renderInvite(locale, msgData{
			GranterName:  "IT Security https://evil.example/login",
			GranterEmail: "x@example.com",
			NodeName:     "Mailbox closing - verify at www.evil.example now",
			Instance:     "pp-hub",
			Level:        store.ShareLevelView,
			Link:         "https://drive.example.com/open/1",
		})
		if strings.Contains(subject, "IT Security") || strings.Contains(subject, "Mailbox") {
			t.Errorf("%s: subject carries sender text: %q", locale, subject)
		}
		if strings.Contains(body, "evil.example") {
			t.Errorf("%s: body carries a sender link: %q", locale, body)
		}
		if !strings.Contains(body, "https://drive.example.com/open/1") {
			t.Errorf("%s: body lost the instance link", locale)
		}
	}
}
