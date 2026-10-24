package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var configuredRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

func validRepo(repo string) bool {
	return configuredRepoPattern.MatchString(strings.TrimSpace(repo))
}

func parsePaySetting(text string) (float64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	amount, err := strconv.ParseFloat(text, 64)
	if err != nil || amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, fmt.Errorf("enter a non-negative amount, or leave blank")
	}
	return amount, nil
}

func configuredLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
