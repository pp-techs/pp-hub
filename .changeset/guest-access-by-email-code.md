---
"@pp-hub/release": minor
---

People with no account can now open what was shared with their email address. Turn on **Admin › Settings › Sharing › Let people without an account open what was shared** (`guest_access`, off by default; it also needs external sharing on and outgoing mail configured). A view-only share to an address with no account mails an invitation that says to enter the address; the sign-in page then emails a six-digit code, and a correct code opens the share for seven days of activity, with view and comment but no spaces, search, settings or history. A later sign-in through Logto with the same address turns the guest into an ordinary account. Migration `0029` adds `users.kind` and `guest_otps`.
