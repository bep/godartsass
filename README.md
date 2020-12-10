## Work in progress and not ready for use.

This is a Go API backed by the native [Dart Sass Embedded](https://github.com/sass/dart-sass-embedded) executable.

The primary motivation for this project is to provide `SCSS` support to [Hugo](https://gohugo.io/). I welcome PRs with bug fixes. I will also consider adding functionality, but please raise an issue discussing it first.

For LibSass bindings in Go, see [GoLibSass](https://github.com/bep/golibsass).

The benchmark below compares [GoLibSass](https://github.com/bep/golibsass) with this library. This is almost twice as fast when running single-threaded, but slower when running with multiple Goroutines. We're communicating with the host process via stdin/stdout, which becomes the serialized bottle neck here. That may be possible to improve, but for most practical applications (including Hugo), this should not matter.

```bash
Transpile/SCSS-16              770µs ± 0%     467µs ± 1%   -39.36%  (p=0.029 n=4+4)
Transpile/SCSS_Parallel-16    92.2µs ± 2%   362.5µs ± 1%  +293.39%  (p=0.029 n=4+4)

name                        old alloc/op   new alloc/op   delta
Transpile/SCSS-16               192B ± 0%     1268B ± 0%  +560.42%  (p=0.029 n=4+4)
Transpile/SCSS_Parallel-16      192B ± 0%     1272B ± 0%  +562.37%  (p=0.029 n=4+4)

name                        old allocs/op  new allocs/op  delta
Transpile/SCSS-16               2.00 ± 0%     19.00 ± 0%  +850.00%  (p=0.029 n=4+4)
Transpile/SCSS_Parallel-16      2.00 ± 0%     19.00 ± 0%  +850.00%  (p=0.029 n=4+4)
```

