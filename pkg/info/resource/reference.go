package resource

type referenceImpl struct {
	kind             string
	name             string
	isOperator       bool
	isEventCollector bool
}

func NewReference(kind, name string) Reference {
	return &referenceImpl{
		kind: kind,
		name: name,
	}
}

func (r referenceImpl) Kind() string {
	return r.kind
}

func (r referenceImpl) Name() string {
	return r.name
}

func (r referenceImpl) IsOperator() bool {
	return r.isOperator
}

func (r *referenceImpl) SetIsOperator() {
	r.isOperator = true
}

func (r referenceImpl) IsEventCollector() bool {
	return r.isEventCollector
}

func (r *referenceImpl) SetIsEventCollector() {
	r.isEventCollector = true
}
