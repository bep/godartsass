package godartsass

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bep/godartsass/internal/embeddedsass"
)

// Options configures a Transpiler.
type Options struct {
	// The path to the Dart Sass wrapper binary, an absolute filename
	// if not in $PATH.
	// If this is not set, we will try 'dart-sass-embedded'
	// (or 'dart-sass-embedded.bat' on Windows) in the OS $PATH.
	// There may be several ways to install this, one would be to
	// download it from here: https://github.com/sass/dart-sass-embedded/releases
	DartSassEmbeddedFilename string

	// Custom resolver to use to resolve imports.
	ImportResolver ImportResolver

	// File paths to use to resolve imports.
	IncludePaths []string

	// Ordered list starting with ImportResolver, then the IncludePaths.
	sassImporters []*embeddedsass.InboundMessage_CompileRequest_Importer
}

func (opts *Options) init() error {
	if opts.DartSassEmbeddedFilename == "" {
		opts.DartSassEmbeddedFilename = defaultDartSassEmbeddedFilename
	}

	if opts.ImportResolver != nil {
		opts.sassImporters = []*embeddedsass.InboundMessage_CompileRequest_Importer{
			{
				Importer: &embeddedsass.InboundMessage_CompileRequest_Importer_ImporterId{
					ImporterId: importerID,
				},
			},
		}
	}

	if opts.IncludePaths != nil {
		for _, p := range opts.IncludePaths {
			opts.sassImporters = append(opts.sassImporters, &embeddedsass.InboundMessage_CompileRequest_Importer{Importer: &embeddedsass.InboundMessage_CompileRequest_Importer_Path{
				Path: filepath.Clean(p),
			}})
		}
	}

	return nil
}

// ImportResolver allows custom import resolution.
// CanonicalizeURL should create a canonical version of the given URL if it's
// able to resolve it, else return an empty string.
// Include scheme if relevant, e.g. 'file://foo/bar.scss'.
// Importers   must ensure that the same canonical URL
// always refers to the same stylesheet.
//
// Load loads the canonicalized URL's content.
// TODO1 consider errors.
type ImportResolver interface {
	CanonicalizeURL(url string) string
	Load(canonicalizedURL string) string
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
	OutputStyleExpanded   OutputStyle = "EXPANDED"
	OutputStyleCompact    OutputStyle = "COMPACT"
	OutputStyleCompressed OutputStyle = "COMPRESSED"

	SourceSyntaxSCSS SourceSyntax = "SCSS"
	SourceSyntaxSASS SourceSyntax = "INDENTED"
	SourceSyntaxCSS  SourceSyntax = "CSS"
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
	case SourceSyntaxSASS, "SASS":
		return SourceSyntaxSASS
	case SourceSyntaxCSS:
		return SourceSyntaxCSS
	default:
		return SourceSyntaxSCSS
	}
}
