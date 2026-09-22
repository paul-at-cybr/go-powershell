// Copyright (c) 2017 Gorillalabs. All rights reserved.

package utils_test

import (
	"testing"

	"github.com/paul-at-cybr/go-powershell/utils"
	"github.com/stretchr/testify/require"
)

func TestRandomStrings(t *testing.T) {
	r1 := utils.CreateRandomString(8)
	r2 := utils.CreateRandomString(8)

	require.NotEqual(t, r1, r2, "Failed to create random strings: The two generated strings are identical.")
	require.Len(t, r1, 16, "Expected the random string to contain 16 characters, but got %d.", len(r1))
}
