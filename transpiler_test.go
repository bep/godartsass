// Copyright 2024 Bjørn Erik Pedersen
// SPDX-License-Identifier: MIT

package godartsass_test

import (
	"bytes"
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/bep/godartsass/v2"
	"github.com/bep/godartsass/v2/internal/godartsasstesting"

	qt "github.com/frankban/quicktest"
)

type testImportResolver struct {
	name         string
	content      string
	sourceSyntax godartsass.SourceSyntax

	failOnCanonicalizeURL bool
	failOnLoad            bool
}

func (t testImportResolver) CanonicalizeURL(url string) (string, error) {
	if t.failOnCanonicalizeURL {
		return "", errors.New("failed")
	}
	if url != t.name {
		return "", nil
	}

	return "file:/my" + t.name + "/scss/" + url + "_myfile.scss", nil
}

func (t testImportResolver) Load(url string) (godartsass.Import, error) {
	if t.failOnLoad {
		return godartsass.Import{}, errors.New("failed")
	}
	if !strings.Contains(url, t.name) {
		panic("protocol error")
	}
	return godartsass.Import{Content: t.content, SourceSyntax: t.sourceSyntax}, nil
}

func TestTranspilerVariants(t *testing.T) {
	c := qt.New(t)

	colorsResolver := testImportResolver{
		name:    "colors",
		content: `$white:    #ffff`,
	}

	resolverIndented := testImportResolver{
		name: "main",
		content: `
#main
    color: blue
`,
		sourceSyntax: godartsass.SourceSyntaxSASS,
	}

	for _, test := range []struct {
		name   string
		opts   godartsass.Options
		args   godartsass.Args
		expect any
	}{
		{"Output style compressed", godartsass.Options{}, godartsass.Args{Source: "div { color: #ccc; }", OutputStyle: godartsass.OutputStyleCompressed}, godartsass.Result{CSS: "div{color:#ccc}"}},
		{"Enable Source Map", godartsass.Options{}, godartsass.Args{Source: "div{color:blue;}", URL: "file://myproject/main.scss", OutputStyle: godartsass.OutputStyleCompressed, EnableSourceMap: true}, godartsass.Result{CSS: "div{color:blue}", SourceMap: "{\"version\":3,\"sourceRoot\":\"\",\"sources\":[\"file://myproject/main.scss\"],\"names\":[],\"mappings\":\"AAAA\"}"}},
		{"Enable Source Map with sources", godartsass.Options{}, godartsass.Args{Source: "div{color:blue;}", URL: "file://myproject/main.scss", OutputStyle: godartsass.OutputStyleCompressed, EnableSourceMap: true, SourceMapIncludeSources: true}, godartsass.Result{CSS: "div{color:blue}", SourceMap: "{\"version\":3,\"sourceRoot\":\"\",\"sources\":[\"file://myproject/main.scss\"],\"names\":[],\"mappings\":\"AAAA\",\"sourcesContent\":[\"div{color:blue;}\"]}"}},
		{"Sass syntax", godartsass.Options{}, godartsass.Args{
			Source: `$font-stack:    Helvetica, sans-serif
$primary-color: #333

body
  font: 100% $font-stack
  color: $primary-color
`,
			OutputStyle:  godartsass.OutputStyleCompressed,
			SourceSyntax: godartsass.SourceSyntaxSASS,
		}, godartsass.Result{CSS: "body{font:100% Helvetica,sans-serif;color:#333}"}},
		{"Import resolver with source map", godartsass.Options{}, godartsass.Args{Source: "@import \"colors\";\ndiv { p { color: $white; } }", EnableSourceMap: true, ImportResolver: colorsResolver}, godartsass.Result{CSS: "div p {\n  color: white;\n}", SourceMap: "{\"version\":3,\"sourceRoot\":\"\",\"sources\":[\"data:;charset=utf-8,@import%20%22colors%22;%0Adiv%20%7B%20p%20%7B%20color:%20$white;%20%7D%20%7D\",\"file:///mycolors/scss/colors_myfile.scss\"],\"names\":[],\"mappings\":\"AACM;EAAI,OCDC\"}"}},
		{"Import resolver with indented source syntax", godartsass.Options{}, godartsass.Args{Source: "@import \"main\";\n", ImportResolver: resolverIndented}, godartsass.Result{CSS: "#main {\n  color: blue;\n}"}},

		// Error cases
		{"Invalid syntax", godartsass.Options{}, godartsass.Args{Source: "div { color: $white; }"}, false},
		{"Import not found", godartsass.Options{}, godartsass.Args{Source: "@import \"foo\""}, false},
		{"Import with ImportResolver, not found", godartsass.Options{}, godartsass.Args{Source: "@import \"foo\"", ImportResolver: colorsResolver}, false},
		{"Error in ImportResolver.CanonicalizeURL", godartsass.Options{}, godartsass.Args{Source: "@import \"colors\";", ImportResolver: testImportResolver{name: "colors", failOnCanonicalizeURL: true}}, false},
		{"Error in ImportResolver.Load", godartsass.Options{}, godartsass.Args{Source: "@import \"colors\";", ImportResolver: testImportResolver{name: "colors", failOnLoad: true}}, false},
		{"Invalid OutputStyle", godartsass.Options{}, godartsass.Args{Source: "a", OutputStyle: "asdf"}, false},
		{"Invalid SourceSyntax", godartsass.Options{}, godartsass.Args{Source: "a", SourceSyntax: "asdf"}, false},
		{"Error logging", godartsass.Options{}, godartsass.Args{Source: `@error "foo";`}, false},
	} {
		test := test
		c.Run(test.name, func(c *qt.C) {
			b, ok := test.expect.(bool)
			shouldFail := ok && !b
			transpiler, clean := newTestTranspiler(c, test.opts)
			defer clean()
			result, err := transpiler.Execute(test.args)
			if shouldFail {
				c.Assert(err, qt.Not(qt.IsNil))
				// Verify that the communication is still up and running.
				_, err2 := transpiler.Execute(test.args)
				c.Assert(err2.Error(), qt.Equals, err.Error())
			} else {
				expectedResult := test.expect.(godartsass.Result)
				c.Assert(err, qt.IsNil)
				// printJSON(result.SourceMap)
				c.Assert(result, qt.Equals, expectedResult)

			}
		})

	}
}

func TestDebugWarn(t *testing.T) {
	c := qt.New(t)

	args := godartsass.Args{
		URL: "/a/b/c.scss",
		Source: `
$color: #333;
body {
	  color: $color;
}

 @debug "foo";
@warn "bar";

`,
	}

	var events []godartsass.LogEvent
	eventHandler := func(e godartsass.LogEvent) {
		events = append(events, e)
	}

	opts := godartsass.Options{
		LogEventHandler: eventHandler,
	}

	transpiler, clean := newTestTranspiler(c, opts)
	defer clean()
	result, err := transpiler.Execute(args)
	c.Assert(err, qt.IsNil)

	c.Assert(result.CSS, qt.Equals, "body {\n  color: #333;\n}")
	c.Assert(events, qt.DeepEquals, []godartsass.LogEvent{
		{Type: 2, Message: "/a/b/c.scss:6:1: foo"},
		{Type: 0, Message: "bar"},
	})
}

func TestIncludePaths(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	colors := filepath.Join(dir1, "_colors.scss")
	content := filepath.Join(dir2, "_content.scss")

	os.WriteFile(colors, []byte(`
$moo:       #f442d1 !default;
`), 0o644)

	os.WriteFile(content, []byte(`
content { color: #ccc; }
`), 0o644)

	c := qt.New(t)
	src := `
@import "colors";
@import "content";
div { p { color: $moo; } }`

	transpiler, clean := newTestTranspiler(c, godartsass.Options{})
	defer clean()

	result, err := transpiler.Execute(
		godartsass.Args{
			Source:       src,
			OutputStyle:  godartsass.OutputStyleCompressed,
			IncludePaths: []string{dir1, dir2},
		},
	)
	c.Assert(err, qt.IsNil)
	c.Assert(result.CSS, qt.Equals, "content{color:#ccc}div p{color:#f442d1}")
}

func TestSilenceDeprecations(t *testing.T) {
	dir1 := t.TempDir()
	colors := filepath.Join(dir1, "_colors.scss")

	os.WriteFile(colors, []byte(`
$moo:       #f442d1 !default;
`), 0o644)

	c := qt.New(t)
	src := `
@import "colors";
div { p { color: $moo; } }`

	var loggedImportDeprecation bool
	transpiler, clean := newTestTranspiler(c, godartsass.Options{
		LogEventHandler: func(e godartsass.LogEvent) {
			if e.DeprecationType == "import" {
				loggedImportDeprecation = true
			}
		},
	})
	defer clean()

	result, err := transpiler.Execute(
		godartsass.Args{
			Source:              src,
			OutputStyle:         godartsass.OutputStyleCompressed,
			IncludePaths:        []string{dir1},
			SilenceDeprecations: []string{"import"},
		},
	)
	c.Assert(err, qt.IsNil)
	c.Assert(loggedImportDeprecation, qt.IsFalse)
	c.Assert(result.CSS, qt.Equals, "div p{color:#f442d1}")
}

func TestSilenceDependencyDeprecations(t *testing.T) {
	dir1 := t.TempDir()
	headings := filepath.Join(dir1, "_headings.scss")

	err := os.WriteFile(headings, []byte(`
@use "sass:color";
h1 { color: rgb(color.channel(#aaa, "red", $space: rgb), 0, 0); }
h2 { color: rgb(color.red(#bbb), 0, 0); } // deprecated
`), 0o644)
	if err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	c := qt.New(t)
	src := `
@use "sass:color";
@use "headings";
h3 { color: rgb(color.channel(#ccc, "red", $space: rgb), 0, 0); }
`

	args := godartsass.Args{
		OutputStyle:  godartsass.OutputStyleCompressed,
		IncludePaths: []string{dir1},
	}

	tests := []struct {
		name                          string
		src                           string
		silenceDependencyDeprecations bool
		expectedLogMessage            string
		expectedResult                string
	}{
		{
			name:                          "A",
			src:                           src,
			silenceDependencyDeprecations: false,
			expectedLogMessage:            "color.red() is deprecated",
			expectedResult:                "h1{color:#a00}h2{color:#b00}h3{color:#c00}",
		},
		{
			name:                          "B",
			src:                           src,
			silenceDependencyDeprecations: true,
			expectedLogMessage:            "",
			expectedResult:                "h1{color:#a00}h2{color:#b00}h3{color:#c00}",
		},
		{
			name:                          "C",
			src:                           src + "h4 { color: rgb(0, color.green(#ddd), 0); }",
			silenceDependencyDeprecations: true,
			expectedLogMessage:            "color.green() is deprecated",
			expectedResult:                "h1{color:#a00}h2{color:#b00}h3{color:#c00}h4{color:#0d0}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args.Source = tt.src
			args.SilenceDependencyDeprecations = tt.silenceDependencyDeprecations
			logMessage := ""
			transpiler, clean := newTestTranspiler(c, godartsass.Options{
				LogEventHandler: func(e godartsass.LogEvent) {
					logMessage = e.Message
				},
			})
			defer clean()

			result, err := transpiler.Execute(args)
			c.Assert(err, qt.IsNil)

			if tt.expectedLogMessage == "" {
				c.Assert(logMessage, qt.Equals, "")
			} else {
				c.Assert(logMessage, qt.Contains, tt.expectedLogMessage)
			}

			c.Assert(result.CSS, qt.Equals, tt.expectedResult)
		})
	}
}

func TestTranspilerParallel(t *testing.T) {
	c := qt.New(t)
	transpiler, clean := newTestTranspiler(c, godartsass.Options{})
	defer clean()
	var wg sync.WaitGroup

	for i := range 20 {
		wg.Add(1)
		go func(num int) {
			defer wg.Done()
			for range 8 {
				src := fmt.Sprintf(`
$primary-color: #%03d;

div { color: $primary-color; }`, num)

				var panicWhen godartsasstesting.PanicWhen
				if num == 3 {
					panicWhen = panicWhen | godartsasstesting.ShouldPanicInSendInbound1
				}
				if num == 8 {
					panicWhen = panicWhen | godartsasstesting.ShouldPanicInNewCall
				}
				if num == 10 {
					panicWhen = panicWhen | godartsasstesting.ShouldPanicInSendInbound2
				}
				args := godartsass.Args{Source: src}
				godartsass.TestingApplyArgsSettings(&args, panicWhen)
				if panicWhen > 0 {
					c.Check(func() { transpiler.Execute(args) }, qt.PanicMatches, ".*ShouldPanicIn.*")
				} else {
					result, err := transpiler.Execute(args)
					c.Check(err, qt.IsNil)
					c.Check(result.CSS, qt.Equals, fmt.Sprintf("div {\n  color: #%03d;\n}", num))
				}
				if c.Failed() {
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestTranspilerParallelImportResolver(t *testing.T) {
	c := qt.New(t)

	createImportResolver := func(width int) godartsass.ImportResolver {
		return testImportResolver{
			name:    "widths",
			content: fmt.Sprintf(`$width:  %d`, width),
		}
	}

	transpiler, clean := newTestTranspiler(c, godartsass.Options{})
	defer clean()

	var wg sync.WaitGroup

	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			for j := range 10 {
				for range 20 {
					args := godartsass.Args{
						OutputStyle:    godartsass.OutputStyleCompressed,
						ImportResolver: createImportResolver(j + i),
						Source: `
@import "widths";

div { p { width: $width; } }`,
					}

					result, err := transpiler.Execute(args)
					c.Check(err, qt.IsNil)
					c.Check(result.CSS, qt.Equals, fmt.Sprintf("div p{width:%d}", j+i))
					if c.Failed() {
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestTranspilerClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		// See https://github.com/sass/dart-sass/issues/2424
		t.Skip("skipping test on Windows")
	}
	c := qt.New(t)
	var errBuff bytes.Buffer
	transpiler, _ := newTestTranspiler(c,
		godartsass.Options{
			Stderr: &errBuff,
			LogEventHandler: func(e godartsass.LogEvent) {
				fmt.Println("LogEvent:", e)
			},
		},
	)

	defer func() {
		fmt.Println("Stderr:", errBuff.String())
	}()

	var wg sync.WaitGroup

	for i := range 10 {
		wg.Add(1)
		go func(gor int) {
			defer wg.Done()
			for j := range 4 {
				src := fmt.Sprintf(`
$primary-color: #%03d;

div { color: $primary-color; }`, gor)

				num := gor + j

				if num == 10 {
					err := transpiler.Close()
					if err != nil {
						c.Check(err, qt.Equals, godartsass.ErrShutdown)
					}
				}

				result, err := transpiler.Execute(godartsass.Args{Source: src})

				if err != nil {
					c.Check(err, qt.Equals, godartsass.ErrShutdown)
				} else {
					c.Check(err, qt.IsNil)
					c.Check(result.CSS, qt.Equals, fmt.Sprintf("div {\n  color: #%03d;\n}", gor))
				}

				if c.Failed() {
					return
				}
			}
		}(i)
	}
	wg.Wait()

	c.Assert(transpiler.IsShutDown(), qt.Equals, true)
}

func BenchmarkTranspiler(b *testing.B) {
	type tester struct {
		sources    []string
		transpiler *godartsass.Transpiler
		clean      func()
	}

	const (
		sassSample = `nav {
	  ul {
		margin: 0;
		padding: 0;
		list-style: none;
	  }

	  li { display: inline-block; }

	  a {
		display: block;
		padding: 6px 12px;
		text-decoration: none;
	  }
	}`
	)

	getSassSource := func() string {
		s := sassSample

		// Append some comment to make it unique.
		comment := randStr(rand.Intn(1234))
		s += "\n\n/*! " + comment + " */"

		return s
	}

	newTester := func(b *testing.B, opts godartsass.Options) tester {
		c := qt.New(b)
		sources := make([]string, b.N)
		for i := 0; i < b.N; i++ {
			sources[i] = getSassSource()
		}
		transpiler, clean := newTestTranspiler(c, opts)

		return tester{
			transpiler: transpiler,
			clean:      clean,
			sources:    sources,
		}
	}

	runBench := func(b *testing.B, t tester) {
		defer t.clean()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_, err := t.transpiler.Execute(godartsass.Args{Source: t.sources[n]})
			if err != nil {
				b.Fatal(err)
			}

		}
	}

	b.Run("SCSS", func(b *testing.B) {
		t := newTester(b, godartsass.Options{})
		runBench(b, t)
	})

	// This is the obviously much slower way of doing it.
	b.Run("Start and Execute", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			t := newTester(b, godartsass.Options{})
			_, err := t.transpiler.Execute(godartsass.Args{Source: t.sources[n]})
			if err != nil {
				b.Fatal(err)
			}
			t.transpiler.Close()
		}
	})

	b.Run("SCSS Parallel", func(b *testing.B) {
		t := newTester(b, godartsass.Options{})

		defer t.clean()
		b.RunParallel(func(pb *testing.PB) {
			n := 0
			for pb.Next() {
				_, err := t.transpiler.Execute(godartsass.Args{Source: t.sources[n]})
				if err != nil {
					b.Fatal(err)
				}
				n++
			}
		})
	})
}

func TestVersion(t *testing.T) {
	c := qt.New(t)

	version, err := godartsass.Version(getSassEmbeddedFilename())
	c.Assert(err, qt.IsNil)
	c.Assert(version, qt.Not(qt.Equals), "")
	c.Assert(strings.HasPrefix(version.ProtocolVersion, "3."), qt.IsTrue, qt.Commentf("got: %q", version.ProtocolVersion))
}

func newTestTranspiler(c *qt.C, opts godartsass.Options) (*godartsass.Transpiler, func()) {
	opts.DartSassEmbeddedFilename = getSassEmbeddedFilename()
	transpiler, err := godartsass.Start(opts)
	c.Assert(err, qt.IsNil)

	return transpiler, func() {
		err := transpiler.Close()
		c.Assert(err, qt.IsNil)
	}
}

func getSassEmbeddedFilename() string {
	// https://github.com/sass/dart-sass/releases
	if filename := os.Getenv("DART_SASS_BINARY"); filename != "" {
		return filename
	}

	return "sass"
}

func randStr(len int) string {
	buff := make([]byte, len)
	crand.Read(buff)
	str := base64.StdEncoding.EncodeToString(buff)
	return str[:len]
}
