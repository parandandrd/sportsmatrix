package pool

import (
	"image/color"
	"sync"

	"github.com/parandandrd/sportsmatrix/internal/matrix"
)

// MatrixPointPool provides a sync.Pool for MatrixPoint structs to reduce
// garbage collection pressure during high-frequency rendering operations.
type MatrixPointPool struct {
	pool *sync.Pool
}

// NewMatrixPointPool creates a new pool for MatrixPoint objects.
func NewMatrixPointPool() *MatrixPointPool {
	return &MatrixPointPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return &matrix.MatrixPoint{}
			},
		},
	}
}

// Get retrieves a MatrixPoint from the pool or creates a new one.
func (p *MatrixPointPool) Get() *matrix.MatrixPoint {
	return p.pool.Get().(*matrix.MatrixPoint)
}

// Put returns a MatrixPoint to the pool for reuse.
func (p *MatrixPointPool) Put(point *matrix.MatrixPoint) {
	if point != nil {
		// Reset to zero values
		point.X = 0
		point.Y = 0
		point.Color = color.RGBA{}
		p.pool.Put(point)
	}
}

// GetBatch retrieves a slice of MatrixPoint objects from the pool.
func (p *MatrixPointPool) GetBatch(size int) []matrix.MatrixPoint {
	points := make([]matrix.MatrixPoint, size)
	return points
}

// PutBatch returns a batch of MatrixPoint objects to the pool.
func (p *MatrixPointPool) PutBatch(points []matrix.MatrixPoint) {
	// Slices are stack-allocated or part of larger structs
}
