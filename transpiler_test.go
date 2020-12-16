package godartsass

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
)

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
	sassSampleTranspiled = "nav ul {\n  margin: 0;\n  padding: 0;\n  list-style: none;\n}\nnav li {\n  display: inline-block;\n}\nnav a {\n  display: block;\n  padding: 6px 12px;\n  text-decoration: none;\n}"
)

type testImportResolver struct {
	name    string
	content string
}

func (t testImportResolver) CanonicalizeURL(url string) string {
	if url != t.name {
		return ""
	}

	return url
}

func (t testImportResolver) Load(url string) string {
	if !strings.Contains(url, t.name) {
		panic("protocol error")
	}
	return t.content
}

func TestTranspilerVariants(t *testing.T) {
	c := qt.New(t)

	colorsResolver := testImportResolver{
		name:    "colors",
		content: `$white:    #ffff`,
	}

	for _, test := range []struct {
		name   string
		tOpts  []TranspilerOption
		eArgs  []ExecuteArg
		expect interface{}
	}{
		{"Output style compressed", nil, []ExecuteArg{WithSource("div { color: #ccc; }"), WithOutputStyle(OutputStyleCompressed)}, "div{color:#ccc}"},
		{"Sass syntax", nil, []ExecuteArg{
			WithSource(`$font-stack:    Helvetica, sans-serif
$primary-color: #333

body
  font: 100% $font-stack
  color: $primary-color
`),
			WithOutputStyle(OutputStyleCompressed),
			WithSourceSyntax(SourceSyntaxSASS),
		}, "body{font:100% Helvetica,sans-serif;color:#333}"},
		{"Import resolver", []TranspilerOption{WithImportResolver(colorsResolver)}, []ExecuteArg{WithSource("@import \"colors\";\ndiv { p { color: $white; } }")}, "div p {\n  color: #ffff;\n}"},

		// Error cases
		{"Invalid syntax", nil, []ExecuteArg{WithSource("div { color: $white; }")}, false},
		{"Import not found", nil, []ExecuteArg{WithSource("@import \"foo\"")}, false},
		{"Invalid OutputStyle", nil, []ExecuteArg{WithSource("a"), WithOutputStyle("asdf")}, false},
		{"Invalid SourceSyntax", nil, []ExecuteArg{WithSource("a"), WithSourceSyntax("asdf")}, false},
	} {

		test := test
		c.Run(test.name, func(c *qt.C) {
			b, ok := test.expect.(bool)
			shouldFail := ok && !b
			transpiler, clean := newTestTranspiler(c, test.tOpts...)
			defer clean()
			result, err := transpiler.Execute(test.eArgs...)
			if shouldFail {
				c.Assert(err, qt.Not(qt.IsNil))
			} else {
				c.Assert(err, qt.IsNil)
				c.Assert(result.CSS, qt.Equals, test.expect)
			}
		})

	}
}

func TestIncludePaths(t *testing.T) {
	dir1, _ := ioutil.TempDir(os.TempDir(), "libsass-test-include-paths-dir1")
	defer os.RemoveAll(dir1)
	dir2, _ := ioutil.TempDir(os.TempDir(), "libsass-test-include-paths-dir2")
	defer os.RemoveAll(dir2)

	colors := filepath.Join(dir1, "_colors.scss")
	content := filepath.Join(dir2, "_content.scss")

	ioutil.WriteFile(colors, []byte(`
$moo:       #f442d1 !default;
`), 0644)

	ioutil.WriteFile(content, []byte(`
content { color: #ccc; }
`), 0644)

	c := qt.New(t)
	src := `
@import "colors";
@import "content";
div { p { color: $moo; } }`

	transpiler, clean := newTestTranspiler(c, WithIncludePaths([]string{dir1, dir2}...))
	defer clean()

	result, err := transpiler.Execute(WithSource(src), WithOutputStyle(OutputStyleCompressed))
	c.Assert(err, qt.IsNil)
	c.Assert(result.CSS, qt.Equals, "content{color:#ccc}div p{color:#f442d1}")
}

func TestTranspilerParallel(t *testing.T) {
	c := qt.New(t)
	transpiler, clean := newTestTranspiler(c)
	defer clean()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(num int) {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				src := fmt.Sprintf(`
$primary-color: #%03d;

div { color: $primary-color; }`, num)

				result, err := transpiler.Execute(WithSource(src))
				c.Check(err, qt.IsNil)
				c.Check(result.CSS, qt.Equals, fmt.Sprintf("div {\n  color: #%03d;\n}", num))
				if c.Failed() {
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkTranspiler(b *testing.B) {
	type tester struct {
		src        string
		expect     string
		transpiler *Transpiler
		clean      func()
	}

	newTester := func(b *testing.B, opts ...TranspilerOption) tester {
		c := qt.New(b)
		transpiler, clean := newTestTranspiler(c)

		return tester{
			transpiler: transpiler,
			clean:      clean,
		}
	}

	runBench := func(b *testing.B, t tester) {
		defer t.clean()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			result, err := t.transpiler.Execute(WithSource(t.src))
			if err != nil {
				b.Fatal(err)
			}
			if result.CSS != t.expect {
				b.Fatalf("Got: %q\n", result.CSS)
			}
		}
	}

	b.Run("SCSS", func(b *testing.B) {
		t := newTester(b)
		t.src = sassSample
		t.expect = sassSampleTranspiled
		runBench(b, t)
	})

	// This is the obviously much slower way of doing it.
	b.Run("Start and Execute", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			t := newTester(b)
			t.src = sassSample
			t.expect = sassSampleTranspiled
			result, err := t.transpiler.Execute(WithSource(t.src))
			if err != nil {
				b.Fatal(err)
			}
			if result.CSS != t.expect {
				b.Fatalf("Got: %q\n", result.CSS)
			}
			t.transpiler.Close()
		}
	})

	b.Run("SCSS Parallel", func(b *testing.B) {
		t := newTester(b)
		t.src = sassSample
		t.expect = sassSampleTranspiled
		defer t.clean()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				result, err := t.transpiler.Execute(WithSource(t.src))
				if err != nil {
					b.Fatal(err)
				}
				if result.CSS != t.expect {
					b.Fatalf("Got: %q\n", result.CSS)
				}
			}
		})
	})
}

func newTestTranspiler(c *qt.C, nopts ...TranspilerOption) (*Transpiler, func()) {
	transpiler, err := Start(append(nopts, WithDartSassEmbeddedExecPath(getSassEmbeddedFilename()))...)
	c.Assert(err, qt.IsNil)

	return transpiler, func() {
		c.Assert(transpiler.Close(), qt.IsNil)
	}
}

func getSassEmbeddedFilename() string {
	// https://github.com/sass/dart-sass-embedded/releases
	if filename := os.Getenv("SASS_EMBEDDED_BINARY"); filename != "" {
		return filename
	}

	return defaultDartSassEmbeddedFilename
}
