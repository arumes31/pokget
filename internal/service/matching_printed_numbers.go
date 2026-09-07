package service

import (
	"regexp"
	"strconv"
	"strings"
)

var collectorFractionPattern = regexp.MustCompile(`(?i)\b([a-z]{0,3}[0-9]{1,4})\s*/\s*([a-z]{0,3}[0-9]{1,4})\b`)

func collectorNumberParts(value string) (string, int, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	index := 0
	for index < len(value) && value[index] >= 'a' && value[index] <= 'z' {
		index++
	}
	if index > 3 || !asciiDigits(value[index:]) {
		return "", 0, false
	}
	number, err := strconv.Atoi(value[index:])
	return value[:index], number, err == nil
}

func collectorNumberKey(value string) string {
	prefix, number, ok := collectorNumberParts(value)
	if !ok {
		return ""
	}
	return prefix + strconv.Itoa(number)
}

// Fractions retain printed layout information that normalization would erase.
// Small fractions used in card rules (such as 1/2) are not collector evidence.
func printedCollectorFractions(text string) map[string]bool {
	values := make(map[string]bool)
	for _, match := range collectorFractionPattern.FindAllStringSubmatch(text, -1) {
		prefix, number, valid := collectorNumberParts(match[1])
		totalPrefix, total, totalValid := collectorNumberParts(match[2])
		if valid && totalValid && prefix == totalPrefix && total >= 10 {
			values[prefix+strconv.Itoa(number)] = true
		}
	}
	return values
}
