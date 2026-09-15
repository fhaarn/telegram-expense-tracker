package category

import "strings"

// Label decorates display text without changing a category's stored name or key.
func Label(name string) string {
	icons := map[string]string{
		"food & drinks": "🍜", "transport": "🚗", "groceries": "🛒", "shopping": "🛍️",
		"bills": "🧾", "entertainment": "🎮", "health": "💊", "other": "📦",
	}
	icon := icons[strings.ToLower(name)]
	if icon == "" {
		icon = "🏷️"
	}
	return icon + " " + name
}
