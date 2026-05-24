package parser

import "strings"

// OSInfo extracts OS name from user agent string.
func OSInfo(ua string) string {
	ua = strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "windows"):
		return "Windows"
	case strings.Contains(ua, "mac os x") || strings.Contains(ua, "macintosh"):
		return "macOS"
	case strings.Contains(ua, "linux"):
		if strings.Contains(ua, "android") {
			return "Android"
		}
		return "Linux"
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad"):
		return "iOS"
	case strings.Contains(ua, "android"):
		return "Android"
	default:
		return "Other"
	}
}

// BrowserInfo extracts browser name from user agent string.
func BrowserInfo(ua string) string {
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "edg"):
		return "Edge"
	case strings.Contains(lower, "chrome") && !strings.Contains(lower, "chromium"):
		return "Chrome"
	case strings.Contains(lower, "firefox"):
		return "Firefox"
	case strings.Contains(lower, "safari") && !strings.Contains(lower, "chrome"):
		return "Safari"
	case strings.Contains(lower, "opera") || strings.Contains(lower, "opr"):
		return "Opera"
	case strings.Contains(lower, "bot") || strings.Contains(lower, "crawl") || strings.Contains(lower, "spider"):
		return "Bot"
	case ua == "" || ua == "-":
		return "Empty"
	default:
		return "Other"
	}
}
