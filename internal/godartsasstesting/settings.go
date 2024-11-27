// Copyright 2024 Bjørn Erik Pedersen
// SPDX-License-Identifier: MIT

package godartsasstesting

import (
	"os"
	"strings"
)

// IsTest reports whether we're running as a test.
var IsTest bool

func init() {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") {
			IsTest = true
			break
		}
	}
}

type PanicWhen uint8

func (p PanicWhen) Has(flag PanicWhen) bool {
	return p&flag != 0
}

func (p PanicWhen) Set(flag PanicWhen) PanicWhen {
	return p | flag
}

const (
	// Used in tests.
	ShouldPanicInNewCall PanicWhen = 1 << iota
	ShouldPanicInSendInbound1
	ShouldPanicInSendInbound2
)
