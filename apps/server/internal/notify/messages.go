package notify

import (
	"fmt"
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

	body = fmt.Sprintf(`%s (%s) shared the %s "%s" with %s in %s on %s.

Access: %s

Open it: %s

--
You received this because something was shared with you on %s.
To stop these emails, turn off "Email me when something is shared with me" in Settings → General.`,
		d.GranterName, d.GranterEmail, itemType, d.NodeName, target, d.SpaceName, d.Instance,
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

	body = fmt.Sprintf(`%s (%s) đã chia sẻ %s "%s" với %s trong %s trên %s.

Quyền: %s

Mở: %s

--
Bạn nhận được email này vì có nội dung được chia sẻ với bạn trên %s.
Để tắt các email này, hãy tắt "Gửi email khi có nội dung được chia sẻ với tôi" trong Cài đặt → Chung.`,
		d.GranterName, d.GranterEmail, itemType, d.NodeName, target, d.SpaceName, d.Instance,
		access,
		d.Link,
		d.Instance,
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
