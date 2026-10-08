package kanban

// Fractional indexing: a position is a string over a 36-letter alphabet read as
// the digits of a fraction, so between any two positions there is another one,
// and ordering them bytewise orders the cards. Moving a card therefore writes
// one row, and two people moving different cards never touch the same rows.
//
// Keys never end in the zero digit, which is what lets a midpoint always exist.
const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

func digit(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	// The alphabet is in byte order, so a letter's index is its offset.
	c := s[i]
	if c >= 'a' {
		return int(c-'a') + 10
	}
	return int(c - '0')
}

// Between returns a position that sorts after a and before b. An empty a means
// the start, an empty b the end. a must sort before b.
func Between(a, b string) string {
	return midpoint(a, b)
}

// midpoint is the fractional-index midpoint of a and b (b "" = one).
func midpoint(a, b string) string {
	if b != "" {
		// Strip the shared prefix: a is read as padded with zeros.
		n := 0
		for n < len(b) && digit(a, n) == digit(b, n) {
			n++
		}
		if n > 0 {
			rest := ""
			if n < len(a) {
				rest = a[n:]
			}
			return b[:n] + midpoint(rest, b[n:])
		}
	}
	da := digit(a, 0)
	db := len(alphabet)
	if b != "" {
		db = digit(b, 0)
	}
	if db-da > 1 {
		return string(alphabet[(da+db+1)/2])
	}
	// Consecutive first digits.
	if len(b) > 1 {
		return b[:1]
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	return string(alphabet[da]) + midpoint(rest, "")
}

// Counter keys are what appending uses: a letter for the number of digits, the
// number in base 36 that wide, and a closing "i" (a last letter other than the
// zero digit keeps a key usable by Between). They sort in counting order, and
// a key gains a letter only when the digits are used up, so 5000 appends make
// a five-letter key rather than one a letter longer every few steps.
const counterTail = 'i'

func counterKey(v int) string {
	width := 1
	for pow := len(alphabet); v >= pow; pow *= len(alphabet) {
		width++
	}
	buf := make([]byte, width+2)
	buf[0] = alphabet[width]
	for j := width; j >= 1; j-- {
		buf[j] = alphabet[v%len(alphabet)]
		v /= len(alphabet)
	}
	buf[width+1] = counterTail
	return string(buf)
}

// counterValue reads a counter key; ok is false for any other key.
func counterValue(s string) (int, bool) {
	if len(s) < 3 || s[len(s)-1] != counterTail {
		return 0, false
	}
	width := digit(s, 0)
	if width < 1 || width > 6 || len(s) != width+2 {
		return 0, false
	}
	v := 0
	for j := 1; j <= width; j++ {
		c := s[j]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z') {
			return 0, false
		}
		v = v*len(alphabet) + digit(s, j)
	}
	return v, true
}

// After returns a position after the last one in a list; "" is an empty list.
func After(last string) string {
	if last == "" {
		return counterKey(1)
	}
	if v, ok := counterValue(last); ok {
		return counterKey(v + 1)
	}
	// The end of the list was placed by hand, between two others.
	return Between(last, "")
}

// MaxPositionLen is the length past which a position is renumbered rather than
// halved further; repeated inserts at one spot lengthen a key by a letter every
// few moves.
const MaxPositionLen = 48

// Spread returns n ordered short positions, for renumbering a list whose keys
// have grown long.
func Spread(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = counterKey(i + 1)
	}
	return out
}
