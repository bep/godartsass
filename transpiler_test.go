package godartsass

import (
	"fmt"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
)

const (
	// https://github.com/sass/dart-sass-embedded/releases
	// TODO1
	dartSassEmbeddedFilename = "/Users/bep/Downloads/sass_embedded/dart-sass-embedded"

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

func TestTranspileSingle(t *testing.T) {
	c := qt.New(t)

	transpiler, clean := newTestTranspiler(c)
	defer clean()

	for i := 0; i < 2; i++ {
		result, err := transpiler.Execute(sassSample)
		c.Assert(err, qt.IsNil)

		c.Assert(result.CSS, qt.Equals, sassSampleTranspiled)
	}
}

func TestTranspileParallel(t *testing.T) {
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

				result, err := transpiler.Execute(src)
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

func newTestTranspiler(c *qt.C) (*Transpiler, func()) {
	transpiler, err := Start(Options{DartSassEmbeddedFilename: dartSassEmbeddedFilename})
	c.Assert(err, qt.IsNil)

	return transpiler, func() {
		c.Assert(transpiler.Close(), qt.IsNil)
	}
}

func BenchmarkTranspile(b *testing.B) {
	type tester struct {
		src        string
		expect     string
		transpiler *Transpiler
		clean      func()
	}

	newTester := func(b *testing.B, opts Options) tester {
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
			result, err := t.transpiler.Execute(t.src)
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

	b.Run("SCSS Parallel", func(b *testing.B) {
		t := newTester(b, Options{})
		t.src = sassSample
		t.expect = sassSampleTranspiled
		defer t.clean()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				result, err := t.transpiler.Execute(t.src)
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
