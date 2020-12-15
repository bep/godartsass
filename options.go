package godartsass

import (
	"fmt"
	"strings"

	"github.com/bep/godartsass/internal/embeddedsass"
)

type Options struct {
	// The path to the Dart Sass wrapper binary, an absolute filename
	// if not in $PATH.
	// If this is not set, we will try 'dart-sass-embedded'
	// (or 'dart-sass-embedded.bat' on Windows) in the OS $PATH.
	// There may be several ways to install this, one would be to
	// download it from here: https://github.com/sass/dart-sass-embedded/releases
	DartSassEmbeddedFilename string

	ImportResolver ImportResolver
}

func (opts Options) createImporters() []*embeddedsass.InboundMessage_CompileRequest_Importer {
	if opts.ImportResolver == nil {
		// No custom import resolver.
		return nil
	}
	return []*embeddedsass.InboundMessage_CompileRequest_Importer{
		&embeddedsass.InboundMessage_CompileRequest_Importer{
			Importer: &embeddedsass.InboundMessage_CompileRequest_Importer_ImporterId{
				ImporterId: importerID,
			},
		},
	}
}

// Args holds the arguments to Execute.
type Args struct {
	// The input source.
	Source string

	// Defaults is SCSS.
	SourceSyntax SourceSyntax

	// Default is NESTED.
	OutputStyle OutputStyle

	sassOutputStyle  embeddedsass.InboundMessage_CompileRequest_OutputStyle
	sassSourceSyntax embeddedsass.InboundMessage_Syntax
}

func (args *Args) init() error {
	if args.OutputStyle == "" {
		args.OutputStyle = OutputStyleNested
	}
	if args.SourceSyntax == "" {
		args.SourceSyntax = SourceSyntaxSCSS
	}

	v, ok := embeddedsass.InboundMessage_CompileRequest_OutputStyle_value[string(args.OutputStyle)]
	if !ok {
		return fmt.Errorf("invalid OutputStyle %q", args.OutputStyle)
	}
	args.sassOutputStyle = embeddedsass.InboundMessage_CompileRequest_OutputStyle(v)

	v, ok = embeddedsass.InboundMessage_Syntax_value[string(args.SourceSyntax)]
	if !ok {
		return fmt.Errorf("invalid SourceSyntax %q", args.SourceSyntax)
	}

	args.sassSourceSyntax = embeddedsass.InboundMessage_Syntax(v)

	return nil
}

type (
	OutputStyle  string
	SourceSyntax string
)

const (
	OutputStyleNested     OutputStyle = "NESTED"
	OutputStyleExpanded               = "EXPANDED"
	OutputStyleCompact                = "COMPACT"
	OutputStyleCompressed             = "COMPRESSED"

	SourceSyntaxSCSS SourceSyntax = "SCSS"
	SourceSyntaxSASS              = "INDENTED"
	SourceSyntaxCSS               = "CSS"
)

// ParseOutputStyle will convert s into OutputStyle.
// Case insensitive, returns OutputStyleNested for unknown value.
func ParseOutputStyle(s string) OutputStyle {
	switch OutputStyle(strings.ToUpper(s)) {
	case OutputStyleNested:
		return OutputStyleNested
	case OutputStyleCompact:
		return OutputStyleCompact
	case OutputStyleCompressed:
		return OutputStyleCompressed
	case OutputStyleExpanded:
		return OutputStyleExpanded
	default:
		return OutputStyleNested
	}
}

// ParseSourceSyntax will convert s into SourceSyntax.
// Case insensitive, returns SourceSyntaxSCSS for unknown value.
func ParseSourceSyntax(s string) SourceSyntax {
	switch SourceSyntax(strings.ToUpper(s)) {
	case SourceSyntaxSCSS:
		return SourceSyntaxSCSS
	case SourceSyntaxSASS:
		return SourceSyntaxSASS
	case SourceSyntaxCSS:
		return SourceSyntaxCSS
	default:
		return SourceSyntaxSCSS
	}
}
