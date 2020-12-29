package godartsass

import (
	"encoding/json"
	"errors"
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

func (t testImportResolver) Load(url string) (string, error) {
	if t.failOnLoad {
		return "", errors.New("failed")
	}
	if !strings.Contains(url, t.name) {
		panic("protocol error")
	}
	return t.content, nil
}

func TestTranspilerVariants(t *testing.T) {
	c := qt.New(t)

	colorsResolver := testImportResolver{
		name:    "colors",
		content: `$white:    #ffff`,
	}

	for _, test := range []struct {
		name   string
		opts   Options
		args   Args
		expect interface{}
	}{
		{"Output style compressed", Options{}, Args{Source: "div { color: #ccc; }", OutputStyle: OutputStyleCompressed}, Result{CSS: "div{color:#ccc}"}},
		{"Enable Source Map", Options{}, Args{Source: "div{color:blue;}", URL: "file://myproject/main.scss", OutputStyle: OutputStyleCompressed, EnableSourceMap: true}, Result{CSS: "div{color:blue}", SourceMap: "{\"version\":3,\"sourceRoot\":\"\",\"sources\":[\"file://myproject/main.scss\"],\"names\":[],\"mappings\":\"AAAA\"}"}},
		{"Sass syntax", Options{}, Args{
			Source: `$font-stack:    Helvetica, sans-serif
$primary-color: #333

body
  font: 100% $font-stack
  color: $primary-color
`,
			OutputStyle:  OutputStyleCompressed,
			SourceSyntax: SourceSyntaxSASS,
		}, Result{CSS: "body{font:100% Helvetica,sans-serif;color:#333}"}},
		{"Import resolver with source map", Options{}, Args{Source: "@import \"colors\";\ndiv { p { color: $white; } }", EnableSourceMap: true, ImportResolver: colorsResolver}, Result{CSS: "div p {\n  color: #ffff;\n}", SourceMap: "{\"version\":3,\"sourceRoot\":\"\",\"sources\":[\"data:;charset=utf-8,@import%20%22colors%22;%0Adiv%20%7B%20p%20%7B%20color:%20$white;%20%7D%20%7D\",\"file:///mycolors/scss/colors_myfile.scss\"],\"names\":[],\"mappings\":\"AACM;EAAI,OCDC\"}"}},

		// Error cases
		{"Invalid syntax", Options{}, Args{Source: "div { color: $white; }"}, false},
		{"Import not found", Options{}, Args{Source: "@import \"foo\""}, false},
		{"Import with ImportResolver, not found", Options{}, Args{Source: "@import \"foo\"", ImportResolver: colorsResolver}, false},
		{"Error in ImportResolver.CanonicalizeURL", Options{}, Args{Source: "@import \"colors\";", ImportResolver: testImportResolver{name: "colors", failOnCanonicalizeURL: true}}, false},
		{"Error in ImportResolver.Load", Options{}, Args{Source: "@import \"colors\";", ImportResolver: testImportResolver{name: "colors", failOnLoad: true}}, false},
		{"Invalid OutputStyle", Options{}, Args{Source: "a", OutputStyle: "asdf"}, false},
		{"Invalid SourceSyntax", Options{}, Args{Source: "a", SourceSyntax: "asdf"}, false},
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
				expectedResult := test.expect.(Result)
				c.Assert(err, qt.IsNil)
				//printJSON(result.SourceMap)
				c.Assert(result, qt.Equals, expectedResult)

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

	transpiler, clean := newTestTranspiler(c, Options{})
	defer clean()

	result, err := transpiler.Execute(
		Args{
			Source:       src,
			OutputStyle:  OutputStyleCompressed,
			IncludePaths: []string{dir1, dir2},
		},
	)
	c.Assert(err, qt.IsNil)
	c.Assert(result.CSS, qt.Equals, "content{color:#ccc}div p{color:#f442d1}")

}

func TestTranspilerParallel(t *testing.T) {
	c := qt.New(t)
	transpiler, clean := newTestTranspiler(c, Options{})
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

				result, err := transpiler.Execute(Args{Source: src})
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

func TestTranspilerParallelImportResolver(t *testing.T) {
	c := qt.New(t)

	createImportResolver := func(width int) ImportResolver {

		return testImportResolver{
			name:    "widths",
			content: fmt.Sprintf(`$width:  %d`, width),
		}

	}

	transpiler, clean := newTestTranspiler(c, Options{})
	defer clean()

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			for j := 0; j < 10; j++ {

				for k := 0; k < 20; k++ {
					args := Args{
						OutputStyle:    OutputStyleCompressed,
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
	c := qt.New(t)
	transpiler, _ := newTestTranspiler(c, Options{})
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(gor int) {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				src := fmt.Sprintf(`
$primary-color: #%03d;

div { color: $primary-color; }`, gor)

				num := gor + j

				if num == 10 {
					err := transpiler.Close()
					if err != nil {
						c.Check(err, qt.Equals, ErrShutdown)
					}
				}

				result, err := transpiler.Execute(Args{Source: src})

				if err != nil {
					c.Check(err, qt.Equals, ErrShutdown)
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

	for _, p := range transpiler.pending {
		c.Assert(p.Error, qt.Equals, ErrShutdown)
	}
}

func BenchmarkTranspiler(b *testing.B) {
	type tester struct {
		src        string
		expect     string
		transpiler *Transpiler
		clean      func()
	}

	newTester := func(b *testing.B, opts Options) tester {
		c := qt.New(b)
		transpiler, clean := newTestTranspiler(c, Options{})

		return tester{
			transpiler: transpiler,
			clean:      clean,
		}
	}

	runBench := func(b *testing.B, t tester) {
		defer t.clean()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			result, err := t.transpiler.Execute(Args{Source: t.src})
			if err != nil {
				b.Fatal(err)
			}
			if result.CSS != t.expect {
				b.Fatalf("Got: %q\n", result.CSS)
			}
		}
	}

	b.Run("SCSS", func(b *testing.B) {
		t := newTester(b, Options{})
		t.src = sassSample
		t.expect = sassSampleTranspiled
		runBench(b, t)
	})

	// This is the obviously much slower way of doing it.
	b.Run("Start and Execute", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			t := newTester(b, Options{})
			t.src = sassSample
			t.expect = sassSampleTranspiled
			result, err := t.transpiler.Execute(Args{Source: t.src})
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
		t := newTester(b, Options{})
		t.src = sassSample
		t.expect = sassSampleTranspiled
		defer t.clean()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				result, err := t.transpiler.Execute(Args{Source: t.src})
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

func TestHasScheme(t *testing.T) {
	c := qt.New(t)

	c.Assert(hasScheme("file:foo"), qt.Equals, true)
	c.Assert(hasScheme("http:foo"), qt.Equals, true)
	c.Assert(hasScheme("http://foo"), qt.Equals, true)
	c.Assert(hasScheme("123:foo"), qt.Equals, false)
	c.Assert(hasScheme("foo"), qt.Equals, false)

}

func newTestTranspiler(c *qt.C, opts Options) (*Transpiler, func()) {
	opts.DartSassEmbeddedFilename = getSassEmbeddedFilename()
	transpiler, err := Start(opts)
	c.Assert(err, qt.IsNil)

	return transpiler, func() {
		err := transpiler.Close()
		c.Assert(err, qt.IsNil)
	}
}

func getSassEmbeddedFilename() string {
	// https://github.com/sass/dart-sass-embedded/releases
	if filename := os.Getenv("DART_SASS_EMBEDDED_BINARY"); filename != "" {
		return filename
	}

	return defaultDartSassEmbeddedFilename
}

// used for debugging
func printJSON(s string) {
	m := make(map[string]interface{})
	json.Unmarshal([]byte(s), &m)
	b, _ := json.MarshalIndent(m, "", "  ")
	fmt.Printf("%s", b)

}
