package resource

type referenceImpl struct {
	kind string
	name string
}

func newReference(kind, name string) Reference {
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
