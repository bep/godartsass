package godartsass

import (
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestHasScheme(t *testing.T) {
	c := qt.New(t)

	c.Assert(hasScheme("file:foo"), qt.Equals, true)
	c.Assert(hasScheme("http:foo"), qt.Equals, true)
	c.Assert(hasScheme("http://foo"), qt.Equals, true)
	c.Assert(hasScheme("123:foo"), qt.Equals, false)
	c.Assert(hasScheme("foo"), qt.Equals, false)
}
