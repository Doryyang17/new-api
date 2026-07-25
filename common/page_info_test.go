package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPageBoundsRejectsOverflowingPage(t *testing.T) {
	start, end := GetPageBounds(25, math.MaxInt, 100)
	assert.Equal(t, 25, start)
	assert.Equal(t, 25, end)

	start, end = GetPageBounds(25, 2, 20)
	assert.Equal(t, 20, start)
	assert.Equal(t, 25, end)
}
