// Copyright (c) 2017 Gorillalabs. All rights reserved.

package utils_test

import (
	"fmt"
	"testing"

	"github.com/paul-at-cybr/go-powershell/utils"
	"github.com/stretchr/testify/require"
)

func TestQuotingArguments(t *testing.T) {
	testcases := [][]string{
		{"", "''"},
		{"test", "'test'"},
		{"two words", "'two words'"},
		{"quo\"ted", "'quo\"ted'"},
		{"quo'ted", "'quo\"ted'"},
		{"quo\\'ted", "'quo\\\"ted'"},
		{"quo\"t'ed", "'quo\"t\"ed'"},
		{"es\\caped", "'es\\caped'"},
		{"es`caped", "'es`caped'"},
		{"es\\`caped", "'es\\`caped'"},
	}

	for i, testcase := range testcases {
		t.Run(fmt.Sprintf("Case #%d", i), func(t *testing.T) {
			require.Equal(t, testcase[1], utils.QuoteArg(testcase[0]))
		})
	}
}
