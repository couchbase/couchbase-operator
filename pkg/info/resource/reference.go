package resource

type resourceReferenceImpl struct {
	kind string
	name string
}

func newResourceReference(kind, name string) ResourceReference {
	return &resourceReferenceImpl{
		kind: kind,
		name: name,
	}
}

func (r resourceReferenceImpl) Kind() string {
	return r.kind
}

func (r resourceReferenceImpl) Name() string {
	return r.name
}
