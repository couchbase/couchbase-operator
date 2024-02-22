package v2

import "testing"

const (
	LowVersionImage      = "couchbase/server:6.5.0"
	LowVersionHashImage  = "couchbase/server@sha256:b21765563ba510c0b1ca43bc9287567761d901b8d00fee704031e8f405bfa501"
	MedVersionImage      = "couchbase/server:7.0.1"
	MedVersionHashImage  = "couchbase/server@sha256:fa5d031059e005cd9d85983b1a120dab37fc60136cb699534b110f49d27388f7"
	HighVersionImage     = "couchbase/server:7.1.3"
	HighVersionHashImage = "couchbase/server@sha256:d0d1734a98fea7639793873d9a54c27d6be6e7838edad2a38e8d451d66be3497"
)

func TestLowestInUseCouchbaseVersionImageOnlyClusterImage(t *testing.T) {
	spec := ClusterSpec{
		Image: HighVersionImage,
	}

	if lowImage, err := spec.LowestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if lowImage != HighVersionImage {
		t.Errorf("expected image to be: %s, but got: %s", HighVersionImage, lowImage)
	}
}

func TestLowestInUseCouchbaseVersionImageLowCluster(t *testing.T) {
	spec := ClusterSpec{
		Image: LowVersionImage,
		Servers: []ServerConfig{
			{
				Image: HighVersionImage,
			},
			{
				Image: LowVersionImage,
			},
		},
	}

	if lowImage, err := spec.LowestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if lowImage != LowVersionImage {
		t.Errorf("expected image to be: %s, but got: %s", LowVersionImage, lowImage)
	}
}

func TestLowestInUseCouchbaseVersionImageHighCluster(t *testing.T) {
	spec := ClusterSpec{
		Image: HighVersionImage,
		Servers: []ServerConfig{
			{
				Image: LowVersionImage,
			},
			{},
			{
				Image: HighVersionImage,
			},
		},
	}

	if lowImage, err := spec.LowestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if lowImage != LowVersionImage {
		t.Errorf("expected image to be: %s, but got: %s", LowVersionImage, lowImage)
	}
}

func TestLowestInUseCouchbaseVersionImageAllSame(t *testing.T) {
	spec := ClusterSpec{
		Image: HighVersionImage,
		Servers: []ServerConfig{
			{
				Image: HighVersionImage,
			},
			{
				Image: HighVersionImage,
			},
			{
				Image: HighVersionImage,
			},
		},
	}

	if lowImage, err := spec.LowestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if lowImage != HighVersionImage {
		t.Errorf("expected image to be: %s, but got: %s", HighVersionImage, lowImage)
	}
}

func TestLowestInUseCouchbaseVersionImageTooManyImages(t *testing.T) {
	spec := ClusterSpec{
		Image: HighVersionImage,
		Servers: []ServerConfig{
			{
				Image: HighVersionImage,
			},
			{
				Image: MedVersionImage,
			},
			{
				Image: LowVersionImage,
			},
		},
	}

	if _, err := spec.LowestInUseCouchbaseVersionImage(); err == nil {
		t.Errorf("should have failed but succeeded")
	}
}

func TestLowestInUseCouchbaseVersionImage256HashImages(t *testing.T) {
	spec := ClusterSpec{
		Image: LowVersionHashImage,
		Servers: []ServerConfig{
			{
				Image: LowVersionHashImage,
			},
			{
				Image: HighVersionHashImage,
			},
		},
	}

	if lowImage, err := spec.LowestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if lowImage != LowVersionHashImage {
		t.Errorf("expected image to be: %s, but got: %s", LowVersionHashImage, lowImage)
	}
}

func TestLowestInUseCouchbaseVersionImageMixedImageTypes(t *testing.T) {
	spec := ClusterSpec{
		Image: LowVersionHashImage,
		Servers: []ServerConfig{
			{
				Image: HighVersionImage,
			},
			{
				Image: LowVersionHashImage,
			},
		},
	}

	if lowImage, err := spec.LowestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if lowImage != LowVersionHashImage {
		t.Errorf("expected image to be: %s, but got: %s", LowVersionHashImage, lowImage)
	}
}

func TestLowestInUseCouchbaseVersionImageOverrideClusterHigh(t *testing.T) {
	spec := ClusterSpec{
		Image: LowVersionImage,
		Servers: []ServerConfig{
			{
				Image: HighVersionImage,
			},
			{
				Image: HighVersionImage,
			},
		},
	}

	if lowImage, err := spec.LowestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if lowImage != HighVersionImage {
		t.Errorf("expected image to be: %s, but got: %s", LowVersionImage, lowImage)
	}
}

func TestLowestInUseCouchbaseVersionImageOverrideClusterLow(t *testing.T) {
	spec := ClusterSpec{
		Image: HighVersionImage,
		Servers: []ServerConfig{
			{
				Image: LowVersionImage,
			},
			{
				Image: LowVersionImage,
			},
		},
	}

	if lowImage, err := spec.LowestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if lowImage != LowVersionImage {
		t.Errorf("expected image to be: %s, but got: %s", LowVersionImage, lowImage)
	}
}

func TestHighestInUseCouchbaseVersionImageOnlyClusterImage(t *testing.T) {
	spec := ClusterSpec{
		Image: HighVersionImage,
	}

	if highImage, err := spec.HighestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if highImage != HighVersionImage {
		t.Errorf("expected image to be: %s, but got: %s", HighVersionImage, highImage)
	}
}

func TestHighestInUseCouchbaseVersionImageLowCluster(t *testing.T) {
	spec := ClusterSpec{
		Image: LowVersionImage,
		Servers: []ServerConfig{
			{
				Image: HighVersionImage,
			},
			{
				Image: LowVersionImage,
			},
		},
	}

	if highImage, err := spec.HighestInUseCouchbaseVersionImage(); err != nil {
		t.Error(err)
	} else if highImage != HighVersionImage {
		t.Errorf("expected image to be: %s, but got: %s", HighVersionImage, highImage)
	}
}
