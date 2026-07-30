# XGo Classfiles For Formula Authors

Use this reference only for XGo syntax and the classfile transformation. Use
the parent `SKILL.md` for the current LLAR Formula API and use the target
package's source for build behavior.

When the `xgo-classfile` skill is available, prefer its complete and
version-aware instructions over this focused reference.

## Contents

- [Sources](#sources)
- [Go compatibility](#go-compatibility)
- [LLAR class relationship](#llar-class-relationship)
- [Filename rules](#filename-rules)
- [File structure](#file-structure)
- [Fields](#fields)
- [Calls](#calls)
- [Lambdas](#lambdas)
- [Error operators](#error-operators)
- [Literals, loops, and imports](#literals-loops-and-imports)
- [Debugging checklist](#debugging-checklist)

## Sources

Treat XGo as a Go-compatible syntactic superset. Start with ordinary Go syntax
and use XGo conveniences deliberately:

- [XGo language guide](https://github.com/goplus/xgo/blob/main/doc/docs.md)
- [XGo classfile guide](https://github.com/goplus/xgo/blob/main/doc/classfile.md)

When generated behavior matters, use the XGo, gogen, and goplus/mod versions
selected by the LLAR revision. Inspect XGo's `cl/classfile.go`, `cl/compile.go`,
and `cl/expr.go` and the embedding registration in LLAR instead of assuming a
different compiler version behaves identically.

## Go Compatibility

Ordinary Go declarations, expressions, statements, control flow, imports,
parenthesized calls, composite literals, and exported identifier spellings are
valid XGo. XGo sugar adds alternatives; it does not replace the Go forms.

XGo also resolves an exported Go identifier through a lowercase-first-letter
alias:

```gox
os.readFile(path)        // os.ReadFile(path)
strings.trimSpace(text)  // strings.TrimSpace(text)
builder.build()          // builder.Build()
```

Only the first ASCII letter changes. The original exported spelling remains
valid. The alias does not expose arbitrary unexported names.

Use the lowercase alias consistently with the surrounding LLAR formulas, but
do not confuse it with a separate method in the Go API.

## LLAR Class Relationship

LLAR registers two XGo project classes through its ixgo build host:

| Suffix | Embedded Go base | Automatic imports |
| --- | --- | --- |
| `_llar.gox` | `formula.ModuleF` | `autotools`, `cmake` |
| `_cmp.gox` | `cmp.CmpApp` | `semver`, `gnu` |

The generated type anonymously embeds the registered base. Go promotion makes
the base's exported methods available as unqualified classfile DSL calls.
There is no separate inheritance runtime.

For a current `Zlib_llar.gox`:

```gox
id "madler/zlib"
fromVer "1.0.0"

onBuild ctx => {
	ctx.setMetadata "-lz"
}
```

the essential generated Go shape is:

```go
package main

type Zlib struct {
	formula.ModuleF
}

func (this *Zlib) MainEntry() {
	this.Id("madler/zlib")
	this.FromVer("1.0.0")
	this.OnBuild(func(ctx *formula.Context) {
		ctx.SetMetadata("-lz")
	})
}

func (this *Zlib) Main() {
	formula.XGot_ModuleF_Main(this)
}

func main() {
	new(Zlib).Main()
}
```

The exact generated source may include compiler details not shown here. The
important contract is that top-level formula statements populate the embedded
`ModuleF`, and the framework entrypoint invokes `MainEntry` for a zero-value
generated class.

Do not declare the generated type, `MainEntry`, `Main`, or package `main`
yourself. Do not call generated `XGot_`, `Gopt_`, `Gops_`, `Gopx_`, `Gopo_`,
or numbered overload names from formula source.

## Filename Rules

The last underscore starts a custom class extension in general XGo classfile
parsing. LLAR additionally obtains the generated formula type name from the
filename prefix before the first underscore.

Use the established unambiguous form:

```text
Zlib_llar.gox  -> class Zlib, extension _llar.gox
Zlib_cmp.gox   -> class Zlib, extension _cmp.gox
```

Keep the prefix a valid Go identifier. Do not derive a class name by trimming
arbitrary suffix text in tooling; use XGo's filename rules and LLAR's actual
loader path.

## File Structure

Imports, constants, named types, field declarations, and helper methods must
appear before the first top-level executable statement. Once the parser enters
the class entry body, the remaining top-level source is treated as executable
classfile code.

```gox
import "strings"

func normalize(value string) string {
	return strings.trimSpace(value)
}

id "owner/repo"
fromVer "v1.0.0"
```

A receiverless top-level function in a classfile becomes a method on the
generated class, not a package function. Unqualified references inside it may
resolve to generated fields, other generated methods, promoted base methods,
package declarations, imports, framework exports, or builtins.

If package-level helpers are genuinely required, put them in an ordinary Go,
XGo, or Gop source file supported by the owning build path. Formula repositories
normally keep the required logic in the classfile.

## Fields

In an ordinary classfile, the first top-level `var` declaration before any
function or executable statement forms generated class fields. Do not
initialize those fields in that declaration. Later `var` declarations are
package variables.

Most formulas need no class fields. Prefer local variables inside hooks unless
state must intentionally be shared by callbacks on the same generated formula
instance.

## Calls

Parenthesized Go calls always remain valid. A call used as a standalone
statement may use command style:

```gox
c.buildType "Release"
c.define "FEATURE", "ON"
deps.require "owner/dependency", "v1.2.3"
```

Use parentheses when consuming a return value or when normal Go expression
syntax is clearer:

```gox
depDir := ctx.outputDir(dep)
data, err := proj.readFile("build.txt")
```

Zero-argument methods can be auto-properties:

```gox
installDir := ctx.outputDir // ctx.OutputDir__0()
if lastErr != nil {        // this.LastErr()
	panic lastErr
}
```

The Go API may encode overloads with `__0`, `__1`, and so on. Call only the XGo
surface name. XGo selects `ctx.outputDir` or `ctx.outputDir(dep)` from the
argument list.

## Lambdas

LLAR hooks use XGo lambda expressions:

```gox
ctx => ctx.setMetadata("-lfoo")
(proj, deps) => {
	deps.require "owner/dependency", "v1.2.3"
}
=> true
```

Multiple parameters require parentheses. A block lambda uses ordinary Go/XGo
statements and must match the function signature supplied by the current LLAR
base class.

Do not infer a hook signature from an older formula. Read the current Go method
on `formula.ModuleF`.

## Error Operators

XGo supports postfix error operators:

| Syntax | Behavior |
| --- | --- |
| `call()!` | Panic when the final error result is non-nil; otherwise yield the non-error result. |
| `call()?` | Return the error from the current function when its signature permits it. |
| `call()?:fallback` | Use the fallback value when the call returns an error. |

Framework behavior remains separate. In current LLAR, Formula hook boundaries
recover panics and return contextual errors, so `proj.readFile(path)!` is
appropriate for a required input. Whether an absent file is optional is a
target-package decision and must be verified before using a fallback.

Build wrapper methods that return no error cannot use a postfix error operator;
their current implementation already panics on failure.

## Literals, Loops, And Imports

Use ordinary Go composite literals and range loops whenever they are clearer.
XGo inferred literals and `for in` forms are also available:

```gox
values := ["first", "second"]
settings := {"mode": "static"}

for value in values {
	_ = value
}
```

When inference is ambiguous, use an explicit Go type or `make`. Standard
library packages are not auto-imported; add Go imports explicitly.

For the full syntax surface, including comprehensions, optional parameters,
keyword arguments, custom iterators, operators, rational numbers, and domain
text literals, use the official XGo language guide. Do not restate or guess
those features in a Formula change when ordinary Go syntax is sufficient.

## Debugging Checklist

1. Confirm the LLAR revision and its XGo/ixgo versions.
2. Confirm `_llar.gox` or `_cmp.gox` registration in
   `internal/ixgo/classfile.go`.
3. Confirm the filename prefix resolves to the generated class LLAR looks up.
4. Map top-level statements to `MainEntry` and hooks to their generated
   function types.
5. Map lowercase aliases, auto-properties, and overloads back to real Go APIs.
6. Compile through LLAR's ixgo loader, not a different XGo CLI path.
7. When a diagnostic mentions generated Go or an XGo AST node, inspect the
   generated source or compiler path before changing Formula behavior.
