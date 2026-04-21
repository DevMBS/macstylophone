//go:build !darwin

package hardware

type GestureSuppressor struct{}

func NewGestureSuppressor() *GestureSuppressor {
	return &GestureSuppressor{}
}

func NewDockOnlyGestureSuppressor() *GestureSuppressor {
	return &GestureSuppressor{}
}

func (g *GestureSuppressor) Start() error {
	return nil
}

func (g *GestureSuppressor) Stop() {}
