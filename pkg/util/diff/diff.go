// package diff provides helper functions to diff arbitrary objects for the purposes
// of debug and logging.
package diff

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/errors"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
)

// Diff takes a pair of objects, marshals them into YAML then generates a string
// diff of them.
func Diff(old, new interface{}) (string, error) {
	oldBytes, err := yaml.Marshal(old)
	if err != nil {
		return "", errors.NewStackTracedError(err)
	}

	newBytes, err := yaml.Marshal(new)
	if err != nil {
		return "", errors.NewStackTracedError(err)
	}

	return cmp.Diff(string(oldBytes), string(newBytes)), nil
}

// PrettyDiff takes a pair of objects and finds the diff and prints using a pretty
// diff that's human readable using a custom reporter with go-cmp.
func PrettyDiff(old, new interface{}) string {
	reporter := diffReporter{}
	cmp.Diff(old, new, cmp.Reporter(&reporter))

	return reporter.String()
}

type diffReporter struct {
	path  cmp.Path
	diffs []string
}

func (r *diffReporter) PushStep(ps cmp.PathStep) {
	r.path = append(r.path, ps)
}

func (r *diffReporter) PopStep() {
	r.path = r.path[:len(r.path)-1]
}

func (r *diffReporter) Report(rs cmp.Result) {
	if !rs.Equal() {
		vx, vy := r.path.Last().Values()

		switch {
		case isNilOrEmpty(vx):
			r.diffs = append(r.diffs, fmt.Sprintf("+%#v:%+v", r.path, vy))
		case isNilOrEmpty(vy):
			r.diffs = append(r.diffs, fmt.Sprintf("-%#v:%+v", r.path, vx))
		default:
			r.diffs = append(r.diffs, fmt.Sprintf("%#v:%+v->%+v", r.path, vx, vy))
		}
	}
}

func (r *diffReporter) String() string {
	return strings.Join(r.diffs, ";")
}

func isNilOrEmpty(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}

	k := v.Kind()
	switch k {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.UnsafePointer, reflect.Interface, reflect.Slice:
		return v.IsNil()
	case reflect.String:
		return v.Len() == 0
	}

	return false
}
