package service

import (
	"strconv"
	"strings"
)

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
