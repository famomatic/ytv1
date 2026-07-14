package cookies

import (
	"bufio"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ParseNetscape parses a Netscape cookies.txt format.
// Format: domain flag path secure expiration name value
func ParseNetscape(r io.Reader) ([]*http.Cookie, error) {
	var cookies []*http.Cookie
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// The de-facto Netscape format (curl, Firefox, yt-dlp) marks HttpOnly
		// cookies with a literal "#HttpOnly_" prefix on the domain field.
		// Treating it as a comment silently drops every HttpOnly auth cookie
		// (YouTube LOGIN_INFO, SID, HSID, SSID, ...), breaking --cookies auth.
		httpOnly := false
		if rest, ok := strings.CutPrefix(line, "#HttpOnly_"); ok {
			httpOnly = true
			line = rest
		} else if strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 7 {
			continue
		}

		// Field 0: Domain
		domain := parts[0]
		// Field 1: Flag (TRUE/FALSE) - treat as string, ignore?
		// Field 2: Path
		path := parts[2]
		// Field 3: Secure (TRUE/FALSE)
		secure := strings.EqualFold(parts[3], "TRUE")
		// Field 4: Expiration (Unix timestamp). A value of 0 (or a
		// non-numeric/blank field) denotes a session cookie; leaving Expires at
		// its zero value keeps it a session cookie. Setting it to time.Unix(0,0)
		// (1970) instead makes net/http/cookiejar drop it as already-expired.
		// Field 5: Name
		name := parts[5]
		// Field 6: Value
		value := parts[6]

		cookie := &http.Cookie{
			Name:     name,
			Value:    value,
			Domain:   domain,
			Path:     path,
			Secure:   secure,
			HttpOnly: httpOnly,
		}
		if expiresUnix, err := strconv.ParseInt(strings.TrimSpace(parts[4]), 10, 64); err == nil && expiresUnix > 0 {
			cookie.Expires = time.Unix(expiresUnix, 0)
		}
		cookies = append(cookies, cookie)
	}

	return cookies, scanner.Err()
}
