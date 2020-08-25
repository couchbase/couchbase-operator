package eventschema

import (
	"fmt"
)

// ErrOverflow is raised when the schema expects more events than provided.
var ErrOverflow = fmt.Errorf("schema overflowed event stream")

// ErrUnderflow is raised when the schema matches fewer events than provided.
var ErrUnderflow = fmt.Errorf("schema underflowed event stream")

// ErrReasonMismatch is raised when event reasons do not match.
var ErrReasonMismatch = fmt.Errorf("event reason mismatch")

// ErrMessageMismatch is raised when event messages do not match.
var ErrMessageMismatch = fmt.Errorf("event message mismatch")

// ErrFuzzyMessageMismatch is raised when event messages do not fuzzy match.
var ErrFuzzyMessageMismatch = fmt.Errorf("event fuzzy message mismatch")

// ErrSetMismatch is raised when no validors match.
var ErrSetMismatch = fmt.Errorf("no set members matched")

// ErrAnyOf is raised when no validators match.
var ErrAnyOf = fmt.Errorf("no anyof members matched")

// ErrRepeatAtLeast is raised when the validor doesn't match or doesn't match
// at least N times.
var ErrRepeatAtLeast = fmt.Errorf("validator doesn't match atleast times")

// ErrRepeatAtMost is raised when the validator doesn't match any or more
// than N sequences.
var ErrRepeatAtMost = fmt.Errorf("validator doesn't match any or atmost times")
