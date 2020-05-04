package util

import (
	"fmt"
)

func UniqueString(strList []string) bool {
	set := map[string]interface{}{}
	for _, str := range strList {
		set[str] = nil
	}
	return len(set) == len(strList)
}

type EnumList []string

func (e EnumList) Contains(s string) bool {
	for _, element := range e {
		if element == s {
			return true
		}
	}
	return false
}

func (e EnumList) Interfaces() []interface{} {
	i := []interface{}{}
	for _, element := range e {
		i = append(i, element)
	}
	return i
}

// StringArrayCompareOrdered compares two arrays and ensure the elements are the same.
func StringArrayCompareOrdered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// StringArrayCompare compares two arrays and ensure the elements are the same
// but unordered.
func StringArrayCompare(a1, a2 []string) bool {
	m := make(map[string]int)
	for _, val := range a1 {
		m[val]++
	}

	for _, val := range a2 {
		if _, ok := m[val]; ok {
			if m[val] > 0 {
				m[val]--
				continue
			}
		}
		return false
	}

	for _, cnt := range m {
		if cnt > 0 {
			return false
		}
	}

	return true
}

func StringPtrEquals(p1, p2 *string) bool {
	return (p1 == nil && p2 == nil) || (p1 != nil && p2 != nil && *p1 == *p2)
}

type UpdateError struct {
	field string
	in    string
}

func NewUpdateError(field, in string) error {
	return &UpdateError{field: field, in: in}
}

func (e *UpdateError) Error() string {
	return fmt.Sprintf("%s in %s cannot be updated", e.field, e.in)
}
