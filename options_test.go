package godartsass

import (
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestParseOutputStyle(t *testing.T) {
	c := qt.New(t)

	c.Assert(ParseOutputStyle("compressed"), qt.Equals, OutputStyleCompressed)
	c.Assert(ParseOutputStyle("ComPressed"), qt.Equals, OutputStyleCompressed)
	c.Assert(ParseOutputStyle("expanded"), qt.Equals, OutputStyleExpanded)
	c.Assert(ParseOutputStyle("foo"), qt.Equals, OutputStyleExpanded)
}

func TestParseSourceSyntax(t *testing.T) {
	c := qt.New(t)

	c.Assert(ParseSourceSyntax("scss"), qt.Equals, SourceSyntaxSCSS)
	c.Assert(ParseSourceSyntax("css"), qt.Equals, SourceSyntaxCSS)
	c.Assert(ParseSourceSyntax("cSS"), qt.Equals, SourceSyntaxCSS)
	c.Assert(ParseSourceSyntax("sass"), qt.Equals, SourceSyntaxSASS)
	c.Assert(ParseSourceSyntax("indented"), qt.Equals, SourceSyntaxSASS)
	c.Assert(ParseSourceSyntax("foo"), qt.Equals, SourceSyntaxSCSS)
}
