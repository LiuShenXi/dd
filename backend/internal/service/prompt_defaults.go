package service

import "strings"

func promptOrDefault(prompt string) string {
	if value := strings.TrimSpace(prompt); value != "" {
		return value
	}
	return "hi"
}
