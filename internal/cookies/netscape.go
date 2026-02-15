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
		if line == "" || strings.HasPrefix(line, "#") {
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
		// Field 4: Expiration (Unix timestamp)
		expiresUnix, _ := strconv.ParseInt(parts[4], 10, 64)
		// Field 5: Name
		name := parts[5]
		// Field 6: Value
		value := parts[6]

		cookie := &http.Cookie{
			Name:     name,
			Value:    value,
			Domain:   domain,
			Path:     path,
			Expires:  time.Unix(expiresUnix, 0),
			Secure:   secure,
			HttpOnly: true, // Generally safe assumption for session cookies? Not stored in file though.
		}
		cookies = append(cookies, cookie)
	}

	return cookies, scanner.Err()
}
