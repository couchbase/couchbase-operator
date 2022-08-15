package couchbaseutil

import (
	"strconv"
)

func BoolToInt(b bool) int {
	return map[bool]int{false: 0, true: 1}[b]
}

func BoolToStr(b bool) string {
	return strconv.Itoa(BoolToInt(b))
}

func BoolAsStr(b bool) string {
	if b {
		return "true"
	}

	return "false"
}

func IntToStr(i int) string {
	return strconv.Itoa(i)
}

func FindFirstCommon(ls1, ls2 []string) string {
	if len(ls1) == 0 || len(ls2) == 0 {
		return ""
	}

	for _, s := range ls1 {
		if contains(ls2, s) {
			return s
		}
	}

	return ""
}

func contains(ls []string, s string) bool {
	for _, a := range ls {
		if a == s {
			return true
		}
	}

	return false
}
