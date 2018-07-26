// See RFC6902: JavaScript Object Notation (JSON) Patch
package jsonpatch

// JsonPatchOperation is a valid operation type
type JsonPatchOperation string

const (
	Add     JsonPatchOperation = "add"
	Remove  JsonPatchOperation = "remove"
	Replace JsonPatchOperation = "replace"
	Move    JsonPatchOperation = "move"
	Copy    JsonPatchOperation = "copy"
	Test    JsonPatchOperation = "test"
)

// JsonPatch defines a valid JSON patch command
type JsonPatch struct {
	Op    JsonPatchOperation `json:"op"`
	Path  string             `json:"path,omitempty"`
	From  string             `json:"from,omitempty"`
	Value interface{}        `json:"value,omitempty"`
}

// JsonPatchList defines a list of JSON patches
type JsonPatchList []JsonPatch
