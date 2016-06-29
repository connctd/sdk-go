package sdk

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

var (
	invalidName1 = "Eigenschaft 1"
	validName1   = "Eigenschaft1"
)

func TestValidNameRegexp(t *testing.T) {
	assert := assert.New(t)

	assert.False(validNameRegexp.MatchString(invalidName1))
	assert.True(validNameRegexp.MatchString(validName1))
}
