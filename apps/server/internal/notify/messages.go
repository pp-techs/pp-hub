package notify

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

type msgData struct {
	GranterName  string
	GranterEmail string
	NodeName     string
	IsDir        bool
	Group        string
	SpaceName    string
	Instance     string
	Level        store.ShareLevel
	CanDownload  bool
	Link         string
	// Guest: the invitation can be opened with a one-time code, no account.
	Guest bool
}

func localeOf(tag string) string {
	if strings.HasPrefix(strings.ToLower(tag), "vi") {
		return "vi"
	}
	return "en"
}

func render(locale string, d msgData) (subject, body string) {
	if locale == "vi" {
		return renderVI(d)
	}
	return renderEN(d)
}

func renderEN(d msgData) (subject, body string) {
	var access string
	if d.Level == store.ShareLevelEdit {
		access = "Can edit"
	} else if d.CanDownload {
		access = "Can view and download"
	} else {
		access = "Can view"
	}

	target := "you"
	if d.Group != "" {
		subject = fmt.Sprintf(`%s shared "%s" with %s`, d.GranterName, d.NodeName, d.Group)
		target = fmt.Sprintf("the group %s, which you belong to,", d.Group)
	} else {
		subject = fmt.Sprintf(`%s shared "%s" with you`, d.GranterName, d.NodeName)
	}

	itemType := "file"
	if d.IsDir {
		itemType = "folder"
	}

	where := ""
	if d.SpaceName != "" {
		where = " in " + d.SpaceName
	}
	body = fmt.Sprintf(`%s (%s) shared the %s "%s" with %s%s on %s.

Access: %s

Open it: %s

--
You received this because something was shared with you on %s.
To stop these emails, turn off "Email me when something is shared with me" in Settings → General.`,
		d.GranterName, d.GranterEmail, itemType, d.NodeName, target, where, d.Instance,
		access,
		d.Link,
		d.Instance,
	)
	return subject, body
}

func renderVI(d msgData) (subject, body string) {
	var access string
	if d.Level == store.ShareLevelEdit {
		access = "Có thể chỉnh sửa"
	} else if d.CanDownload {
		access = "Có thể xem và tải xuống"
	} else {
		access = "Chỉ xem"
	}

	target := "bạn"
	if d.Group != "" {
		subject = fmt.Sprintf(`%s đã chia sẻ "%s" với nhóm %s`, d.GranterName, d.NodeName, d.Group)
		target = fmt.Sprintf("nhóm %s mà bạn là thành viên", d.Group)
	} else {
		subject = fmt.Sprintf(`%s đã chia sẻ "%s" với bạn`, d.GranterName, d.NodeName)
	}

	itemType := "tệp"
	if d.IsDir {
		itemType = "thư mục"
	}

	where := ""
	if d.SpaceName != "" {
		where = " trong " + d.SpaceName
	}
	body = fmt.Sprintf(`%s (%s) đã chia sẻ %s "%s" với %s%s trên %s.

Quyền: %s

Mở: %s

--
Bạn nhận được email này vì có nội dung được chia sẻ với bạn trên %s.
Để tắt các email này, hãy tắt "Gửi email khi có nội dung được chia sẻ với tôi" trong Cài đặt → Chung.`,
		d.GranterName, d.GranterEmail, itemType, d.NodeName, target, where, d.Instance,
		access,
		d.Link,
		d.Instance,
	)
	return subject, body
}

// renderInvite is the mail to an address with no account: somebody shared
// something with it, and signing up with this very address is how to open
// it. There is no opt-out line — there is no account to carry a preference.
//
// The recipient never asked for it and the sender chose every word of their
// name and the file's, so the subject carries none of them, and in the body
// both are clipped and stripped of links (untrusted): an invitation must not
// be a phishing mail signed with this instance's name.
func renderInvite(locale string, d msgData) (subject, body string) {
	d.GranterName = untrusted(d.GranterName)
	d.NodeName = untrusted(d.NodeName)
	if locale == "vi" {
		return renderInviteVI(d)
	}
	return renderInviteEN(d)
}

var linkish = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+.-]*://|www\.)\S*`)

// untrusted clips sender-chosen text and replaces anything that reads as a
// link, so a mail client has nothing of theirs to make clickable.
func untrusted(s string) string {
	s = linkish.ReplaceAllString(s, "[link removed]")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}

func renderInviteEN(d msgData) (subject, body string) {
	access := "Can view"
	if d.Level == store.ShareLevelEdit {
		access = "Can edit"
	} else if d.CanDownload {
		access = "Can view and download"
	}
	itemType := "file"
	if d.IsDir {
		itemType = "folder"
	}
	where := ""
	if d.SpaceName != "" {
		where = " in " + d.SpaceName
	}
	how := "To open it, create an account with this email address and verify it: " + d.Link
	if d.Guest {
		how = "To open it, go to " + d.Link + " and enter this email address; we will send you a one-time code. No account is needed."
	}
	subject = fmt.Sprintf(`A %s was shared with you on %s`, itemType, d.Instance)
	body = fmt.Sprintf(`%s (%s) shared the %s "%s"%s on %s with this address.

Access: %s

%s

--
You received this because %s typed this address into %s. If you do not know them, ignore this email.`,
		d.GranterName, d.GranterEmail, itemType, d.NodeName, where, d.Instance,
		access,
		how,
		d.GranterEmail, d.Instance,
	)
	return subject, body
}

func renderInviteVI(d msgData) (subject, body string) {
	access := "Chỉ xem"
	if d.Level == store.ShareLevelEdit {
		access = "Có thể chỉnh sửa"
	} else if d.CanDownload {
		access = "Có thể xem và tải xuống"
	}
	itemType := "tệp"
	if d.IsDir {
		itemType = "thư mục"
	}
	where := ""
	if d.SpaceName != "" {
		where = " trong " + d.SpaceName
	}
	how := "Để mở, hãy tạo tài khoản bằng chính địa chỉ email này và xác minh nó: " + d.Link
	if d.Guest {
		how = "Để mở, hãy truy cập " + d.Link + " và nhập chính địa chỉ email này; chúng tôi sẽ gửi cho bạn một mã dùng một lần. Không cần tạo tài khoản."
	}
	subject = fmt.Sprintf(`Có %s được chia sẻ với bạn trên %s`, itemType, d.Instance)
	body = fmt.Sprintf(`%s (%s) đã chia sẻ %s "%s"%s trên %s với địa chỉ email này.

Quyền: %s

%s

--
Bạn nhận được email này vì %s đã nhập địa chỉ này trên %s. Nếu bạn không biết người này, hãy bỏ qua email.`,
		d.GranterName, d.GranterEmail, itemType, d.NodeName, where, d.Instance,
		access,
		how,
		d.GranterEmail, d.Instance,
	)
	return subject, body
}

// alertData is a security alert as the email renders it. Times are UTC and
// already formatted; Codes is the joined list of failure reasons.
type alertData struct {
	Instance      string
	IP            string
	Attempts      int64
	WindowMinutes int
	FirstAt       string
	LastAt        string
	Codes         string
	Link          string
}

func renderAlert(locale string, d alertData) (subject, body string) {
	if locale == "vi" {
		return renderAlertVI(d)
	}
	return renderAlertEN(d)
}

func renderAlertEN(d alertData) (subject, body string) {
	subject = fmt.Sprintf("[%s] Security alert: %d failed sign-ins from %s", d.Instance, d.Attempts, d.IP)
	body = fmt.Sprintf(`%d failed sign-in attempts came from the address %s within %d minutes (first at %s, last at %s UTC).

Failure reasons: %s

Review the security log: %s

--
You received this because you are an administrator of %s.`,
		d.Attempts, d.IP, d.WindowMinutes, d.FirstAt, d.LastAt,
		d.Codes,
		d.Link,
		d.Instance,
	)
	return subject, body
}

func renderAlertVI(d alertData) (subject, body string) {
	subject = fmt.Sprintf("[%s] Cảnh báo bảo mật: %d lần đăng nhập thất bại từ %s", d.Instance, d.Attempts, d.IP)
	body = fmt.Sprintf(`Có %d lần đăng nhập thất bại từ địa chỉ %s trong vòng %d phút (lần đầu lúc %s, lần cuối lúc %s UTC).

Lý do thất bại: %s

Xem nhật ký bảo mật: %s

--
Bạn nhận được email này vì bạn là quản trị viên của %s.`,
		d.Attempts, d.IP, d.WindowMinutes, d.FirstAt, d.LastAt,
		d.Codes,
		d.Link,
		d.Instance,
	)
	return subject, body
}
