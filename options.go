package godartsass

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bep/godartsass/internal/embeddedsass"
)

// transpilerOptions configures a Transpiler.  transpilerOptions are set by the
// TranspilerOption values passed to Start.
type transpilerOptions struct {
	// The path to the Dart Sass wrapper binary, an absolute filename
	// if not in $PATH.
	// If this is not set, we will try 'dart-sass-embedded'
	// (or 'dart-sass-embedded.bat' on Windows) in the OS $PATH.
	// There may be several ways to install this, one would be to
	// download it from here: https://github.com/sass/dart-sass-embedded/releases
	dartSassEmbeddedExecPath string

	// Custom resolver to use to resolve imports.
	importResolver ImportResolver

	// File paths to use to resolve imports.
	includePaths []string

	// Ordered list starting with ImportResolver, then the IncludePaths.
	sassImporters []*embeddedsass.InboundMessage_CompileRequest_Importer
}

func (opts *transpilerOptions) init() error {
	if opts.dartSassEmbeddedExecPath == "" {
		opts.dartSassEmbeddedExecPath = defaultDartSassEmbeddedFilename
	}

	if opts.importResolver != nil {
		opts.sassImporters = []*embeddedsass.InboundMessage_CompileRequest_Importer{
			{
				Importer: &embeddedsass.InboundMessage_CompileRequest_Importer_ImporterId{
					ImporterId: importerID,
				},
			},
		}
	}

	if opts.includePaths != nil {
		for _, p := range opts.includePaths {
			opts.sassImporters = append(opts.sassImporters, &embeddedsass.InboundMessage_CompileRequest_Importer{Importer: &embeddedsass.InboundMessage_CompileRequest_Importer_Path{
				Path: filepath.Clean(p),
			}})
		}
	}

	return nil
}

// TranspilerOption configures how the transpiler works.
type TranspilerOption interface {
	apply(*transpilerOptions)
}

// funcTranspilerOption wraps a function that modifies transpilerOptions into an
// implementation of the TranspilerOption interface.
type funcTranspilerOption struct {
	f func(*transpilerOptions)
}

func (f *funcTranspilerOption) apply(o *transpilerOptions) {
	f.f(o)
}

func newFuncTranspilerOption(f func(*transpilerOptions)) *funcTranspilerOption {
	return &funcTranspilerOption{
		f: f,
	}
}

// WithDartSassEmbeddedExecPath returns a TranspilerOption that sets the path to
// the dart-sass-embedded executable.
func WithDartSassEmbeddedExecPath(path string) TranspilerOption {
	return newFuncTranspilerOption(func(o *transpilerOptions) {
		o.dartSassEmbeddedExecPath = path
	})
}

// WithIncludePaths returns a TranspilerOption that sets the file paths used to
// resolve imports.
func WithIncludePaths(paths ...string) TranspilerOption {
	return newFuncTranspilerOption(func(o *transpilerOptions) {
		o.includePaths = paths[:]
	})
}

// WithImportResolver returns a TranspilerOption that sets a custom import path
// resolver.
func WithImportResolver(resolver ImportResolver) TranspilerOption {
	return newFuncTranspilerOption(func(o *transpilerOptions) {
		o.importResolver = resolver
	})
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

// ExecuteArg sets arguments for Transpiler.Execute.
type ExecuteArg interface {
	apply(*executeArgs)
}

// funcExecuteArg wraps a function that modifies executeArgs into an
// implementation of the ExecuteArg interface.
type funcExecuteArg struct {
	f func(*executeArgs)
}

func (f *funcExecuteArg) apply(o *executeArgs) {
	f.f(o)
}

func newFuncExecuteArg(f func(*executeArgs)) *funcExecuteArg {
	return &funcExecuteArg{
		f: f,
	}
}

// WithOutputStyle returns an ExecuteArg that sets the output style for a given
// execution.
func WithOutputStyle(style OutputStyle) ExecuteArg {
	return newFuncExecuteArg(func(o *executeArgs) {
		o.outputStyle = style
	})
}

// WithSource returns an ExecuteArg that sets the source on which the execution
// should operate.
func WithSource(source string) ExecuteArg {
	return newFuncExecuteArg(func(o *executeArgs) {
		o.source = source
	})
}

// WithSourceSyntax returns an ExecuteArg that specifies the source syntax.
func WithSourceSyntax(syntax SourceSyntax) ExecuteArg {
	return newFuncExecuteArg(func(o *executeArgs) {
		o.sourceSyntax = syntax
	})
}

// executeArgs holds the arguments to Execute.
type executeArgs struct {
	// The input source.
	source string

	// Defaults is SCSS.
	sourceSyntax SourceSyntax

	// Default is NESTED.
	outputStyle OutputStyle

	sassOutputStyle  embeddedsass.InboundMessage_CompileRequest_OutputStyle
	sassSourceSyntax embeddedsass.InboundMessage_Syntax
}

func (args *executeArgs) init() error {
	if args.outputStyle == "" {
		args.outputStyle = OutputStyleNested
	}
	if args.sourceSyntax == "" {
		args.sourceSyntax = SourceSyntaxSCSS
	}

	v, ok := embeddedsass.InboundMessage_CompileRequest_OutputStyle_value[string(args.outputStyle)]
	if !ok {
		return fmt.Errorf("invalid OutputStyle %q", args.outputStyle)
	}
	args.sassOutputStyle = embeddedsass.InboundMessage_CompileRequest_OutputStyle(v)

	v, ok = embeddedsass.InboundMessage_Syntax_value[string(args.sourceSyntax)]
	if !ok {
		return fmt.Errorf("invalid SourceSyntax %q", args.sourceSyntax)
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
)

const (
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
