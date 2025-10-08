package utils

import "strings"

// ContainsString checks if a string slice contains a specific string
func ContainsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ContainsSubstring checks if a string contains a substring
func ContainsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}
