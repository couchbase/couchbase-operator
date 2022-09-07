package revision

var gitRevision = ""

// Revision returns the SCM revision information for development builds to
// pinpoint the exact source code tree a defect was raised against.  If the
// information is not available we default to an official release.
func Revision() string {
	return gitRevision
}
